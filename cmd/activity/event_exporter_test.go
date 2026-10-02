package main

import (
	"strings"
	"testing"
)

func TestEventExporterOptionsValidate(t *testing.T) {
	tests := []struct {
		name      string
		planeType string
		cluster   string
		wantErr   bool
	}{
		{name: "valid federated cluster", planeType: "edge", cluster: "cluster-dfw-1"},
		{name: "dotted cluster name", planeType: "edge", cluster: "cluster.dfw.1", wantErr: true},
		{name: "management plane: no cluster", planeType: "", cluster: ""},
		{name: "edge plane without cluster", planeType: "edge", cluster: ""},
		// Not federated, so the name never reaches a subject.
		{name: "cluster set without plane type", planeType: "", cluster: "cluster.dfw.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &EventExporterOptions{PlaneType: tt.planeType, ClusterName: tt.cluster}
			err := o.Validate()
			if tt.wantErr != (err != nil) {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), tt.cluster) {
				t.Errorf("error %q does not name the offending cluster %q", err, tt.cluster)
			}
		})
	}
}
