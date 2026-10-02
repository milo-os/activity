package activityprocessor

import (
	"testing"

	"go.miloapis.com/activity/internal/natsconn"
)

func TestUsesSeparateOutputBroker(t *testing.T) {
	input := natsconn.Endpoint{
		URL: "nats://localhost:4222",
		TLS: natsconn.TLSFiles{Enabled: true, CertFile: "/in/tls.crt", KeyFile: "/in/tls.key", CAFile: "/in/ca.crt"},
	}

	tests := []struct {
		name   string
		output natsconn.Endpoint
		want   bool
	}{
		{
			name:   "no output configuration publishes on the input connection",
			output: natsconn.Endpoint{},
			want:   false,
		},
		{
			name:   "output TLS without a URL still publishes on the input connection",
			output: natsconn.Endpoint{TLS: natsconn.TLSFiles{Enabled: true, CAFile: "/out/ca.crt"}},
			want:   false,
		},
		{
			name:   "an output URL selects a second broker",
			output: natsconn.Endpoint{URL: "nats://hub:4222"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Input: input, Output: tt.output}
			if got := c.usesSeparateOutputBroker(); got != tt.want {
				t.Errorf("usesSeparateOutputBroker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputEndpoint(t *testing.T) {
	inputTLS := natsconn.TLSFiles{Enabled: true, CertFile: "/in/tls.crt", KeyFile: "/in/tls.key", CAFile: "/in/ca.crt"}
	input := natsconn.Endpoint{URL: "nats://localhost:4222", TLS: inputTLS}

	tests := []struct {
		name   string
		output natsconn.Endpoint
		want   natsconn.Endpoint
	}{
		{
			name:   "output without TLS inherits the input's",
			output: natsconn.Endpoint{URL: "nats://hub:4222"},
			want:   natsconn.Endpoint{URL: "nats://hub:4222", TLS: inputTLS},
		},
		{
			name:   "an output CA keeps the input's client identity",
			output: natsconn.Endpoint{URL: "nats://hub:4222", TLS: natsconn.TLSFiles{Enabled: true, CAFile: "/out/ca.crt"}},
			want: natsconn.Endpoint{
				URL: "nats://hub:4222",
				TLS: natsconn.TLSFiles{Enabled: true, CertFile: "/in/tls.crt", KeyFile: "/in/tls.key", CAFile: "/out/ca.crt"},
			},
		},
		{
			name: "a fully specified output inherits nothing",
			output: natsconn.Endpoint{
				URL: "nats://hub:4222",
				TLS: natsconn.TLSFiles{Enabled: true, CertFile: "/out/tls.crt", KeyFile: "/out/tls.key", CAFile: "/out/ca.crt"},
			},
			want: natsconn.Endpoint{
				URL: "nats://hub:4222",
				TLS: natsconn.TLSFiles{Enabled: true, CertFile: "/out/tls.crt", KeyFile: "/out/tls.key", CAFile: "/out/ca.crt"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Input: input, Output: tt.output}
			if got := c.outputEndpoint(); got != tt.want {
				t.Errorf("outputEndpoint() = %+v, want %+v", got, tt.want)
			}
			if c.Input != input {
				t.Errorf("input endpoint mutated: %+v", c.Input)
			}
		})
	}
}

func TestDefaultConfigHasNoOutputBroker(t *testing.T) {
	if DefaultConfig().usesSeparateOutputBroker() {
		t.Error("the default configuration must publish on the input connection")
	}
}
