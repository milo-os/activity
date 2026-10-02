package types

import (
	"fmt"
	"regexp"
)

// clusterNameRE matches a single DNS label. Karmada Cluster names allow dots,
// which would shift the subject token count in EventSubject and the grants
// derived from it if interpolated unchecked.
var clusterNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// ValidClusterName reports whether name may be interpolated into the event
// subjects and inbox prefixes below. The canonical rule for every component
// that derives or matches those subjects.
func ValidClusterName(name string) bool {
	return clusterNameRE.MatchString(name)
}

// EventSubject builds the NATS publish subject for an exported event:
// <prefix>.<cluster>.<namespace>, or <prefix>.<namespace> when cluster is
// empty. Shared with the nkey generator so the publish grants and the
// published subjects cannot drift apart.
func EventSubject(prefix, cluster, namespace string) string {
	if cluster == "" {
		return fmt.Sprintf("%s.%s", prefix, namespace)
	}
	return fmt.Sprintf("%s.%s.%s", prefix, cluster, namespace)
}

// EventSubjectGrant returns the NATS publish grant scoping a leaf cluster to
// its own event subjects.
func EventSubjectGrant(prefix, cluster string) string {
	return fmt.Sprintf("%s.%s.*", prefix, cluster)
}

// EventInboxPrefix returns the NATS inbox prefix a federated cluster's event
// exporter must use for reply subjects (PubAcks and JetStream API replies).
//
// The underscore is load-bearing: "_INBOX_<cluster>" is a distinct first
// subject token from "_INBOX", so a grant on one cannot match the other.
// "_INBOX.<cluster>" would put every cluster back under the hub's "_INBOX.>".
func EventInboxPrefix(cluster string) string {
	return "_INBOX_" + cluster
}

// EventInboxGrant returns the NATS subscribe grant scoping a leaf cluster to
// its own reply inboxes.
func EventInboxGrant(cluster string) string {
	return EventInboxPrefix(cluster) + ".>"
}
