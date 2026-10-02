package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"go.miloapis.com/activity/internal/nkeygenerator"
)

// NkeyGeneratorOptions contains configuration for the nkey generator.
type NkeyGeneratorOptions struct {
	Namespace         string
	KarmadaKubeconfig string
	SecretPrefix      string
	ConfigMapName     string
	SecretStoreName   string
	SecretStoreKind   string
	SubjectPrefix     string
}

// NewNkeyGeneratorOptions creates options with default values.
func NewNkeyGeneratorOptions() *NkeyGeneratorOptions {
	return &NkeyGeneratorOptions{
		Namespace:         getEnvOrDefault("NAMESPACE", "activity-system"),
		KarmadaKubeconfig: os.Getenv("KARMADA_KUBECONFIG"),
		SecretPrefix:      getEnvOrDefault("SECRET_PREFIX", "activity-leaf-nkey"),
		ConfigMapName:     getEnvOrDefault("AUTHORIZED_LEAFS_CONFIGMAP", "nats-activity-authorized-leafs"),
		SecretStoreName:   getEnvOrDefault("SECRET_STORE_NAME", "activity-secret-store"),
		SecretStoreKind:   getEnvOrDefault("SECRET_STORE_KIND", "SecretStore"),
		SubjectPrefix:     getEnvOrDefault("SUBJECT_PREFIX", "activity.federated"),
	}
}

// AddFlags adds nkey generator flags to the flag set.
func (o *NkeyGeneratorOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Namespace, "namespace", o.Namespace, "Namespace holding the per-cluster NKey Secrets, PushSecrets, and the authorized-leafs ConfigMap")
	fs.StringVar(&o.KarmadaKubeconfig, "karmada-kubeconfig", o.KarmadaKubeconfig, "Path to a secretless kubeconfig for the Karmada API (required)")
	fs.StringVar(&o.SecretPrefix, "secret-prefix", o.SecretPrefix, "Prefix for the per-cluster NKey Secret and PushSecret names")
	fs.StringVar(&o.ConfigMapName, "authorized-leafs-configmap", o.ConfigMapName, "Name of the authorized-leafs ConfigMap the hub HelmRelease reads via valuesFrom")
	fs.StringVar(&o.SecretStoreName, "secret-store-name", o.SecretStoreName, "Name of the ESO SecretStore/ClusterSecretStore PushSecrets target")
	fs.StringVar(&o.SecretStoreKind, "secret-store-kind", o.SecretStoreKind, "Kind of the ESO secret store PushSecrets target: SecretStore or ClusterSecretStore")
	fs.StringVar(&o.SubjectPrefix, "subject-prefix", o.SubjectPrefix, "NATS subject prefix the per-cluster publish grants are derived from; must match the event exporter's")
}

// NewNkeyGeneratorCommand creates the nkey-generator subcommand.
func NewNkeyGeneratorCommand() *cobra.Command {
	options := NewNkeyGeneratorOptions()

	cmd := &cobra.Command{
		Use:   "nkey-generator",
		Short: "Provision per-edge-cluster NATS NKey credentials for the federated-events hub",
		Long: `Provision per-edge-cluster NATS NKey credentials for the activity
federated-events hub. For every Karmada Cluster labelled
activity.miloapis.com/nats-leaf=enabled, this generates and persists a NATS
NKey seed (never regenerating an existing one), mirrors it to GCP Secret
Manager via an ESO PushSecret, and rewrites the authorized-leafs ConfigMap
that the hub's HelmRelease reads via valuesFrom.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.KarmadaKubeconfig == "" {
				return fmt.Errorf("--karmada-kubeconfig is required")
			}

			cfg := nkeygenerator.Config{
				Namespace:         options.Namespace,
				KarmadaKubeconfig: options.KarmadaKubeconfig,
				SecretPrefix:      options.SecretPrefix,
				ConfigMapName:     options.ConfigMapName,
				SecretStoreName:   options.SecretStoreName,
				SecretStoreKind:   options.SecretStoreKind,
				SubjectPrefix:     options.SubjectPrefix,
			}
			return nkeygenerator.Run(cmd.Context(), cfg)
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}
