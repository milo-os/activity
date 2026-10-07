package eventexporter

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRegisterMetricsOnlyExporterFamilies(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	if err := registerMetrics(reg); err != nil {
		t.Fatal(err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) == 0 {
		t.Fatal("no metric families registered")
	}
	for _, f := range families {
		if !strings.HasPrefix(f.GetName(), "event_exporter_") {
			t.Errorf("exporter registers foreign metric %s", f.GetName())
		}
	}
}
