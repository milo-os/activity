package edgeingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// ErrDownstreamNamespaceLeak indicates a rewritten event still mentions a
// downstream namespace. It is a bug guard rather than an expected outcome: the
// record is dropped rather than published with an internal identifier in it.
var ErrDownstreamNamespaceLeak = errors.New("rewritten audit event still references a downstream namespace")

// downstreamNamespacePattern matches the ns-<uid> namespaces Milo projects into
// downstream clusters. The UID is a Kubernetes UID, i.e. a UUID, so the pattern
// is specific enough not to catch a customer namespace that merely starts with
// "ns-".
var downstreamNamespacePattern = regexp.MustCompile(`(?i)ns-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// namespacesResource is the resource name an audit record carries when the
// request targeted a namespace object itself.
const namespacesResource = "namespaces"

// DownstreamNamespaceReferences returns every distinct downstream namespace an
// audit event mentions, in sorted order.
//
// A record can name a downstream namespace in five places, and requestURI is
// the one that gets missed because the namespace sits inside a path rather than
// in a field of its own:
//
//  1. objectRef.namespace
//  2. objectRef.name, when the request targeted a namespace object
//  3. requestURI, both in the /namespaces/<name>/ path segment and in query
//     parameters such as fieldSelector=metadata.namespace=<name>
//  4. requestObject, at metadata.namespace of the object and of every nested
//     item, and at metadata.name for a namespace object
//  5. responseObject, in the same places as requestObject
//
// Scanning the serialized event rather than those five fields individually
// means a namespace reference in a place not listed above is still found.
func DownstreamNamespaceReferences(event *auditv1.Event) ([]string, error) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed encoding audit event: %w", err)
	}

	seen := map[string]struct{}{}
	for _, match := range downstreamNamespacePattern.FindAllString(string(encoded), -1) {
		seen[strings.ToLower(match)] = struct{}{}
	}

	references := make([]string, 0, len(seen))
	for reference := range seen {
		references = append(references, reference)
	}
	sort.Strings(references)

	return references, nil
}

// PrimaryDownstreamNamespace returns the namespace an event should be
// attributed to, and whether the event names one at all. A record that names
// none is a cluster-scoped or infrastructure request and belongs to the
// platform.
func PrimaryDownstreamNamespace(event *auditv1.Event) (string, bool) {
	if event.ObjectRef != nil {
		if IsDownstreamNamespace(event.ObjectRef.Namespace) {
			return event.ObjectRef.Namespace, true
		}
		if event.ObjectRef.Resource == namespacesResource && IsDownstreamNamespace(event.ObjectRef.Name) {
			return event.ObjectRef.Name, true
		}
	}

	if namespace, ok := namespaceFromRequestURI(event.RequestURI); ok {
		return namespace, true
	}

	return "", false
}

// namespaceFromRequestURI pulls the namespace out of a request path, falling
// back to a namespace named in a query parameter for collection requests that
// scope themselves with a field selector.
func namespaceFromRequestURI(requestURI string) (string, bool) {
	if requestURI == "" {
		return "", false
	}

	parsed, err := url.ParseRequestURI(requestURI)
	if err != nil {
		if match := downstreamNamespacePattern.FindString(requestURI); match != "" {
			return match, true
		}
		return "", false
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, segment := range segments {
		if segment != namespacesResource || i+1 >= len(segments) {
			continue
		}
		if IsDownstreamNamespace(segments[i+1]) {
			return segments[i+1], true
		}
	}

	for _, values := range parsed.Query() {
		for _, value := range values {
			if match := downstreamNamespacePattern.FindString(value); match != "" {
				return match, true
			}
		}
	}

	return "", false
}

// UnmapNamespaces rewrites every downstream namespace reference in the event to
// the upstream namespace it was projected from, then verifies none survived.
//
// A mapping must be supplied for every reference the event makes. Rewriting
// only the ones we recognise would publish the rest.
func UnmapNamespaces(event *auditv1.Event, mapping map[string]string) error {
	if len(mapping) == 0 {
		return verifyNoDownstreamNamespaces(event)
	}

	if event.ObjectRef != nil {
		if upstream, ok := mapping[strings.ToLower(event.ObjectRef.Namespace)]; ok {
			event.ObjectRef.Namespace = upstream
		}
		if event.ObjectRef.Resource == namespacesResource {
			if upstream, ok := mapping[strings.ToLower(event.ObjectRef.Name)]; ok {
				event.ObjectRef.Name = upstream
			}
		}
	}

	event.RequestURI = replaceNamespaceTokens(event.RequestURI, mapping)

	var err error
	if event.RequestObject, err = rewriteUnknown(event.RequestObject, mapping); err != nil {
		return fmt.Errorf("failed rewriting requestObject: %w", err)
	}
	if event.ResponseObject, err = rewriteUnknown(event.ResponseObject, mapping); err != nil {
		return fmt.Errorf("failed rewriting responseObject: %w", err)
	}

	for key, value := range event.Annotations {
		event.Annotations[key] = replaceNamespaceTokens(value, mapping)
	}

	return verifyNoDownstreamNamespaces(event)
}

// rewriteUnknown rewrites namespace references inside an embedded object.
// The object is walked rather than string-replaced so that a malformed body
// cannot silently pass through unrewritten.
func rewriteUnknown(unknown *runtime.Unknown, mapping map[string]string) (*runtime.Unknown, error) {
	if unknown == nil || len(unknown.Raw) == 0 {
		return unknown, nil
	}

	var decoded any
	if err := json.Unmarshal(unknown.Raw, &decoded); err != nil {
		return nil, fmt.Errorf("embedded object is not valid JSON: %w", err)
	}

	rewritten := rewriteValue(decoded, mapping)

	raw, err := json.Marshal(rewritten)
	if err != nil {
		return nil, fmt.Errorf("failed re-encoding embedded object: %w", err)
	}

	out := unknown.DeepCopy()
	out.Raw = raw
	return out, nil
}

// rewriteValue walks a decoded JSON value and rewrites namespace references in
// every string it contains, at any depth. Walking everything covers
// metadata.namespace on nested list items and on objects the caller has no
// schema for, which a field-by-field rewrite would miss.
func rewriteValue(value any, mapping map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			typed[key] = rewriteValue(nested, mapping)
		}
		return typed
	case []any:
		for i, nested := range typed {
			typed[i] = rewriteValue(nested, mapping)
		}
		return typed
	case string:
		return replaceNamespaceTokens(typed, mapping)
	default:
		return value
	}
}

// replaceNamespaceTokens replaces every mapped downstream namespace token in s,
// including tokens embedded in a longer string such as a self link.
func replaceNamespaceTokens(s string, mapping map[string]string) string {
	if s == "" || !strings.Contains(s, downstreamNamespacePrefix) {
		return s
	}

	return downstreamNamespacePattern.ReplaceAllStringFunc(s, func(match string) string {
		if upstream, ok := mapping[strings.ToLower(match)]; ok {
			return upstream
		}
		return match
	})
}

// verifyNoDownstreamNamespaces is the last gate before an event is emitted.
func verifyNoDownstreamNamespaces(event *auditv1.Event) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed encoding audit event: %w", err)
	}

	if match := downstreamNamespacePattern.FindString(string(encoded)); match != "" {
		return fmt.Errorf("%w: %s", ErrDownstreamNamespaceLeak, match)
	}

	return nil
}
