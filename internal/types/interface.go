package types

import (
	"strings"
)

// Interface types: how a change was made, as reported by the client.
const (
	InterfaceConsole    = "console"
	InterfaceCLI        = "cli"
	InterfaceMCP        = "mcp"
	InterfaceAPI        = "api"
	InterfaceController = "controller"
	InterfaceTerraform  = "terraform"
	InterfaceUnknown    = "unknown"
)

// ClientServiceAccount is the interface client recorded for changes made by a
// Milo ServiceAccount through the API.
const ClientServiceAccount = "service-account"

// serviceAccountUsernameSuffix ends the username Milo's authenticator gives a
// ServiceAccount: "<name>@<project>.identity.miloapis.com" (see
// auth-provider-zitadel internal/controller/serviceaccount_controller.go
// computeEmailAddress, and datum-mcp internal/api/identity.go). Deployments
// that configure a different --email-address-suffix still end in this
// domain, e.g. "<name>@<project>.staging.identity.miloapis.com".
const serviceAccountUsernameSuffix = ".identity.miloapis.com"

// maxClientLength caps the client name taken from a user agent.
const maxClientLength = 64

// IsMiloServiceAccount reports whether username is a Milo ServiceAccount's
// username, "<name>@<project>.identity.miloapis.com". Kubernetes service
// accounts ("system:serviceaccount:...") are not Milo ServiceAccounts.
func IsMiloServiceAccount(username string) bool {
	at := strings.LastIndexByte(username, '@')
	return at > 0 && strings.HasSuffix(strings.ToLower(username[at:]), serviceAccountUsernameSuffix)
}

// ClassifyInterface reports how a change was made, from the request's
// username and user agent. The result is reported by the client, not
// verified: user agents are trivially set to anything, so it must never be
// used for authorization or alerting. Rules, first match wins:
//
//  1. "system:" usernames are controllers. The client is the service in
//     "system:control@<svc>", "<svc>/<x>" for "system:<x>@<svc>", the name
//     in "system:serviceaccount:<ns>:<name>", and otherwise the first segment
//     after "system:" (e.g. "kube-controller-manager", "node").
//  2. Milo ServiceAccounts are "api" with client "service-account", unless
//     the user agent says Terraform.
//  3. User agents starting "datum-mcp", "datum-<x>-mcp" or "compute-mcp" are
//     "mcp", the client being that product name.
//  4. "datumctl" and "datumctl-<plugin>" are "cli"/"datumctl"; "kubectl/" is
//     "cli"/"kubectl".
//  5. "Terraform/" or "terraform-provider-" is "terraform"/"terraform".
//  6. "Mozilla/" (a browser, i.e. the console) is "console".
//  7. Any other user agent is "api", the client being its first product
//     name (e.g. "go-http-client", "axios", "curl"); an empty user agent is
//     "unknown".
//
// Client names are lowercased and reduced to [a-z0-9._/-], at most 64
// characters, so they are safe to show and to facet on.
//
// Follow-ups that would make this more accurate live in other repos:
// auth-provider-zitadel could add the token's OAuth client_id to
// UserInfo.Extra (a server-asserted signal for console, CLI and MCP);
// graphql-gateway could forward the browser's user agent so console changes
// are not labelled by the gateway's HTTP client; the assistant should set its
// own user agent; and datum-mcp should set a user agent and use its own
// OAuth client.
func ClassifyInterface(username, userAgent string) (interfaceType, client string) {
	if strings.HasPrefix(username, "system:") {
		return InterfaceController, controllerClient(username)
	}

	product := productName(userAgent)
	lower := strings.ToLower(product)
	terraform := lower == "terraform" || strings.HasPrefix(lower, "terraform-provider-")

	if IsMiloServiceAccount(username) {
		if terraform {
			return InterfaceTerraform, "terraform"
		}
		return InterfaceAPI, ClientServiceAccount
	}

	switch {
	case strings.TrimSpace(userAgent) == "":
		return InterfaceUnknown, ""
	case isMCPProduct(lower):
		return InterfaceMCP, sanitizeClient(lower)
	case lower == "datumctl" || strings.HasPrefix(lower, "datumctl-"):
		return InterfaceCLI, "datumctl"
	case lower == "kubectl" && strings.Contains(firstToken(userAgent), "/"):
		return InterfaceCLI, "kubectl"
	case terraform:
		return InterfaceTerraform, "terraform"
	case lower == "mozilla" && strings.Contains(firstToken(userAgent), "/"):
		return InterfaceConsole, ""
	default:
		return InterfaceAPI, sanitizeClient(lower)
	}
}

// controllerClient extracts a client name from a "system:" username.
func controllerClient(username string) string {
	rest := strings.TrimPrefix(username, "system:")
	if strings.HasPrefix(rest, "serviceaccount:") {
		parts := strings.Split(rest, ":")
		return sanitizeClient(strings.ToLower(parts[len(parts)-1]))
	}
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		component, svc := rest[:at], trimServiceDomain(rest[at+1:])
		if component == "control" || component == "" {
			return sanitizeClient(strings.ToLower(svc))
		}
		return sanitizeClient(strings.ToLower(svc + "/" + component))
	}
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		rest = rest[:colon]
	}
	return sanitizeClient(strings.ToLower(rest))
}

// trimServiceDomain drops the platform API domains from a service name, so
// "compute.datumapis.com" becomes "compute" and
// "email.notifications.miloapis.com" becomes "email.notifications".
func trimServiceDomain(svc string) string {
	for _, suffix := range []string{".miloapis.com", ".datumapis.com"} {
		if strings.HasSuffix(svc, suffix) && len(svc) > len(suffix) {
			return strings.TrimSuffix(svc, suffix)
		}
	}
	return svc
}

// isMCPProduct reports whether a lowercased product name is one of Datum's
// MCP servers: "datum-mcp", "datum-<x>-mcp" or "compute-mcp".
func isMCPProduct(p string) bool {
	return p == "datum-mcp" || p == "compute-mcp" ||
		(strings.HasPrefix(p, "datum-") && strings.HasSuffix(p, "-mcp") && len(p) > len("datum--mcp"))
}

// firstToken returns the user agent's first whitespace-separated token.
func firstToken(userAgent string) string {
	fields := strings.Fields(userAgent)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// productName returns the product name of the user agent's first token: the
// part before any "/" ("kubectl" in "kubectl/v1.31.0 (linux/amd64) ...").
func productName(userAgent string) string {
	tok := firstToken(userAgent)
	if slash := strings.IndexByte(tok, '/'); slash >= 0 {
		tok = tok[:slash]
	}
	return tok
}

// sanitizeClient keeps only [a-z0-9._/-] from an already lowercased name and
// caps it at maxClientLength.
func sanitizeClient(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' {
			b.WriteRune(r)
			if b.Len() >= maxClientLength {
				break
			}
		}
	}
	return b.String()
}
