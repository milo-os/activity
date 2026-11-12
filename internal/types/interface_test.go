package types

import (
	"strings"
	"testing"
)

func TestClassifyInterface(t *testing.T) {
	const human = "alice@example.com"
	const sa = "deployer@my-project.identity.miloapis.com"

	cases := []struct {
		name       string
		username   string
		userAgent  string
		wantType   string
		wantClient string
	}{
		// Rule 1: controllers.
		{"control identity", "system:control@compute.datumapis.com", "compute-controller/v1", InterfaceController, "compute"},
		{"control identity, nested service", "system:control@email.notifications.miloapis.com", "", InterfaceController, "email.notifications"},
		{"named component", "system:processor@activity.miloapis.com", "Go-http-client/1.1", InterfaceController, "activity/processor"},
		{"control on another domain", "system:control@datum.net", "", InterfaceController, "datum.net"},
		{"kubernetes service account", "system:serviceaccount:kube-system:deployment-controller", "kube-controller-manager/v1.31.0", InterfaceController, "deployment-controller"},
		{"kubernetes component", "system:kube-controller-manager", "", InterfaceController, "kube-controller-manager"},
		{"node", "system:node:worker-1", "kubelet/v1.31.0", InterfaceController, "node"},
		{"controller with browser UA is still a controller", "system:control@networking.datumapis.com", "Mozilla/5.0", InterfaceController, "networking"},

		// Rule 2: Milo service accounts.
		{"service account", sa, "Go-http-client/1.1", InterfaceAPI, ClientServiceAccount},
		{"service account via datumctl", sa, "datumctl/v0.9.0 (darwin/arm64) kubernetes/abc", InterfaceAPI, ClientServiceAccount},
		{"service account, no UA", sa, "", InterfaceAPI, ClientServiceAccount},
		{"service account via terraform", sa, "Terraform/1.9.5 (+https://www.terraform.io) terraform-provider-datum/0.3.0", InterfaceTerraform, "terraform"},
		{"service account via terraform provider", sa, "terraform-provider-datum/0.3.0", InterfaceTerraform, "terraform"},
		{"service account on staging domain", "ci@p.staging.identity.miloapis.com", "curl/8.4.0", InterfaceAPI, ClientServiceAccount},

		// Rule 3: MCP servers.
		{"datum-mcp", human, "datum-mcp/v0.0.0 (darwin/arm64) kubernetes/$Format", InterfaceMCP, "datum-mcp"},
		{"datum-network-mcp", human, "datum-network-mcp", InterfaceMCP, "datum-network-mcp"},
		{"compute-mcp", human, "compute-mcp/1.2.0", InterfaceMCP, "compute-mcp"},
		{"not quite datum-<x>-mcp", human, "datum--mcp", InterfaceAPI, "datum--mcp"},

		// Rule 4: CLIs.
		{"datumctl", human, "datumctl/v0.9.0 (darwin/arm64) kubernetes/abc", InterfaceCLI, "datumctl"},
		{"bare datumctl", human, "datumctl", InterfaceCLI, "datumctl"},
		{"datumctl plugin", human, "datumctl-alb", InterfaceCLI, "datumctl"},
		{"kubectl", human, "kubectl/v1.31.0 (linux/amd64) kubernetes/9edcffc", InterfaceCLI, "kubectl"},

		// Rule 5: Terraform.
		{"terraform", human, "Terraform/1.9.5 (+https://www.terraform.io) terraform-provider-datum/0.3.0", InterfaceTerraform, "terraform"},
		{"terraform provider", human, "terraform-provider-datum/dev", InterfaceTerraform, "terraform"},

		// Rule 6: browsers.
		{"chrome", human, "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36", InterfaceConsole, ""},
		{"firefox", human, "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", InterfaceConsole, ""},

		// Rule 7: everything else.
		{"go http client", human, "Go-http-client/1.1", InterfaceAPI, "go-http-client"},
		{"axios", human, "axios/1.6.8", InterfaceAPI, "axios"},
		{"curl", human, "curl/8.4.0", InterfaceAPI, "curl"},
		{"empty", human, "", InterfaceUnknown, ""},
		{"whitespace", human, "   ", InterfaceUnknown, ""},
		{"garbage", human, "<script>alert(1)</script>", InterfaceAPI, "scriptalert1"},
		{"control characters", human, "\x00\x1b[31mevil", InterfaceAPI, "31mevil"},
		{"kubectl without version", human, "kubectl", InterfaceAPI, "kubectl"},
		{"mozilla without version", human, "Mozilla", InterfaceAPI, "mozilla"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotClient := ClassifyInterface(tc.username, tc.userAgent)
			if gotType != tc.wantType || gotClient != tc.wantClient {
				t.Errorf("ClassifyInterface(%q, %q) = (%q, %q), want (%q, %q)",
					tc.username, tc.userAgent, gotType, gotClient, tc.wantType, tc.wantClient)
			}
		})
	}
}

func TestClassifyInterface_ClientIsCapped(t *testing.T) {
	_, client := ClassifyInterface("alice@example.com", strings.Repeat("a", 500)+"/1.0")
	if len(client) != maxClientLength {
		t.Fatalf("len(client) = %d, want %d", len(client), maxClientLength)
	}
}

func TestIsMiloServiceAccount(t *testing.T) {
	for username, want := range map[string]bool{
		"deployer@my-project.identity.miloapis.com":          true,
		"Deployer@My-Project.IDENTITY.MILOAPIS.COM":          true,
		"ci@p.staging.identity.miloapis.com":                 true,
		"alice@example.com":                                  false,
		"alice@identity.miloapis.com":                        false, // no project label
		"system:serviceaccount:ns:sa":                        false,
		"identity.miloapis.com":                              false,
		"x@evil.com?.identity.miloapis.com.attacker.example": false,
	} {
		if got := IsMiloServiceAccount(username); got != want {
			t.Errorf("IsMiloServiceAccount(%q) = %v, want %v", username, got, want)
		}
	}
}
