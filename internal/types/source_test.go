package types

import "testing"

func TestPrefixWithSource(t *testing.T) {
	tests := []struct {
		name      string
		planeType string
		cluster   string
		id        string
		want      string
	}{
		{
			name:      "plane and cluster set: qualified",
			planeType: "edge",
			cluster:   "cluster-dfw-1",
			id:        "a1b2c3",
			want:      "edge/cluster-dfw-1/a1b2c3",
		},
		{
			name: "neither set: unchanged",
			id:   "a1b2c3",
			want: "a1b2c3",
		},
		{
			name:      "plane only: unchanged",
			planeType: "edge",
			id:        "a1b2c3",
			want:      "a1b2c3",
		},
		{
			name:    "cluster only: unchanged",
			cluster: "cluster-dfw-1",
			id:      "a1b2c3",
			want:    "a1b2c3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrefixWithSource(tt.planeType, tt.cluster, tt.id); got != tt.want {
				t.Errorf("PrefixWithSource(%q, %q, %q) = %q, want %q", tt.planeType, tt.cluster, tt.id, got, tt.want)
			}
		})
	}
}
