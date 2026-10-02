package main

import (
	"testing"

	"github.com/spf13/pflag"

	"go.miloapis.com/activity/internal/natsconn"
)

// TestProcessorNATSFlagBinding pins each --nats-* and --output-nats-* flag to
// the endpoint it configures; distinct paths make a cross-wired pair fail.
func TestProcessorNATSFlagBinding(t *testing.T) {
	o := NewProcessorOptions()
	fs := pflag.NewFlagSet("processor", pflag.ContinueOnError)
	o.AddFlags(fs)

	err := fs.Parse([]string{
		"--nats-url=nats://in:4222",
		"--nats-tls-enabled",
		"--nats-tls-cert-file=/in/tls.crt",
		"--nats-tls-key-file=/in/tls.key",
		"--nats-tls-ca-file=/in/ca.crt",
		"--output-nats-url=nats://out:4222",
		"--output-nats-tls-enabled",
		"--output-nats-tls-cert-file=/out/tls.crt",
		"--output-nats-tls-key-file=/out/tls.key",
		"--output-nats-tls-ca-file=/out/ca.crt",
	})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	wantInput := natsconn.Endpoint{
		URL: "nats://in:4222",
		TLS: natsconn.TLSFiles{Enabled: true, CertFile: "/in/tls.crt", KeyFile: "/in/tls.key", CAFile: "/in/ca.crt"},
	}
	if o.Input != wantInput {
		t.Errorf("Input = %+v, want %+v", o.Input, wantInput)
	}

	wantOutput := natsconn.Endpoint{
		URL: "nats://out:4222",
		TLS: natsconn.TLSFiles{Enabled: true, CertFile: "/out/tls.crt", KeyFile: "/out/tls.key", CAFile: "/out/ca.crt"},
	}
	if o.Output != wantOutput {
		t.Errorf("Output = %+v, want %+v", o.Output, wantOutput)
	}
}

func TestProcessorNATSFlagDefaults(t *testing.T) {
	o := NewProcessorOptions()
	fs := pflag.NewFlagSet("processor", pflag.ContinueOnError)
	o.AddFlags(fs)

	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	if want := (natsconn.Endpoint{URL: "nats://localhost:4222"}); o.Input != want {
		t.Errorf("Input = %+v, want %+v", o.Input, want)
	}
	// An unconfigured output is what makes the processor use one connection.
	if (o.Output != natsconn.Endpoint{}) {
		t.Errorf("Output = %+v, want the zero endpoint", o.Output)
	}
}
