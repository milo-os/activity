package main

import (
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Every subcommand serves the shared registry, so a component registering at
// import time leaks its metrics into the others' /metrics (e.g. the exporter
// serving activity_processor_nats_connection_status 0).
func TestNoComponentMetricsRegisteredAtImport(t *testing.T) {
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		for _, prefix := range []string{"activity_processor_", "event_exporter_", "activity_controller_"} {
			if strings.HasPrefix(f.GetName(), prefix) {
				t.Errorf("%s registered before any subcommand ran", f.GetName())
			}
		}
	}
}
