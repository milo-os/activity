package edgeingest

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"

	"sigs.k8s.io/yaml"
)

// ErrUnknownClient indicates a client certificate that no registry entry claims.
var ErrUnknownClient = errors.New("no edge cluster is registered for the presented client identity")

// ClusterIdentity is everything the ingest path knows about the cluster a
// record came from. Both fields come from the registry, never from a payload.
type ClusterIdentity struct {
	// Name is the Karmada cluster name of the edge control plane.
	Name string `json:"name"`

	// Location is an opaque location identifier, e.g. "us-east-1". Activity
	// stores it verbatim and takes no dependency on any locations service;
	// infrastructure derives it from the Karmada cluster's
	// topology.datum.net/location label when it renders this registry.
	Location string `json:"location"`
}

// registryEntry binds an authenticated client identity to a cluster.
type registryEntry struct {
	// ClientCommonName is the subject common name of the client certificate the
	// edge shipper authenticates with.
	ClientCommonName string `json:"clientCommonName"`

	ClusterIdentity `json:",inline"`
}

type registryFile struct {
	Clusters []registryEntry `json:"clusters"`
}

// ClusterRegistry maps authenticated client identities to edge clusters.
//
// Identity comes from the transport credential alone. A shipper that wants to
// be a different cluster, or claim a different location, has to be issued a
// different certificate — it cannot assert its way there through a request
// body.
type ClusterRegistry struct {
	mu       sync.RWMutex
	byCommon map[string]ClusterIdentity
}

// NewClusterRegistry returns a registry populated from the given entries,
// keyed by client certificate common name.
func NewClusterRegistry(identities map[string]ClusterIdentity) *ClusterRegistry {
	registry := &ClusterRegistry{byCommon: map[string]ClusterIdentity{}}
	for commonName, identity := range identities {
		registry.byCommon[commonName] = identity
	}
	return registry
}

// LoadClusterRegistry reads a registry from a YAML file:
//
//	clusters:
//	  - clientCommonName: audit-shipper.dfw1.edge.datum.net
//	    name: dfw1
//	    location: us-central-1
func LoadClusterRegistry(path string) (*ClusterRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed reading cluster registry: %w", err)
	}

	var file registryFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed parsing cluster registry %s: %w", path, err)
	}

	if len(file.Clusters) == 0 {
		return nil, fmt.Errorf("cluster registry %s registers no clusters", path)
	}

	identities := make(map[string]ClusterIdentity, len(file.Clusters))
	for i, entry := range file.Clusters {
		switch {
		case entry.ClientCommonName == "":
			return nil, fmt.Errorf("cluster registry %s entry %d has no clientCommonName", path, i)
		case entry.Name == "":
			return nil, fmt.Errorf("cluster registry %s entry %d has no name", path, i)
		case entry.Location == "":
			return nil, fmt.Errorf("cluster registry %s entry %d has no location", path, i)
		}
		if _, duplicate := identities[entry.ClientCommonName]; duplicate {
			return nil, fmt.Errorf("cluster registry %s registers %q twice", path, entry.ClientCommonName)
		}
		identities[entry.ClientCommonName] = entry.ClusterIdentity
	}

	return NewClusterRegistry(identities), nil
}

// Reload replaces the registry contents from path, so a rotated ConfigMap takes
// effect without a restart.
func (r *ClusterRegistry) Reload(path string) error {
	reloaded, err := LoadClusterRegistry(path)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byCommon = reloaded.byCommon
	return nil
}

// Len returns the number of registered clusters.
func (r *ClusterRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byCommon)
}

// Identify returns the cluster the given verified client certificate chain
// speaks for.
func (r *ClusterRegistry) Identify(verifiedChains [][]*x509.Certificate) (ClusterIdentity, error) {
	if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
		return ClusterIdentity{}, fmt.Errorf("%w: request presented no verified client certificate", ErrUnknownClient)
	}

	commonName := verifiedChains[0][0].Subject.CommonName

	r.mu.RLock()
	identity, ok := r.byCommon[commonName]
	r.mu.RUnlock()

	if !ok {
		return ClusterIdentity{}, fmt.Errorf("%w: %q", ErrUnknownClient, commonName)
	}

	return identity, nil
}
