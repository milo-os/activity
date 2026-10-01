package edgeingest

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// UpstreamClusters starts a namespace informer against every context in a
// kubeconfig and feeds what it sees into an index.
//
// Watching upstream project control planes rather than the edge clusters keeps
// the reverse lookup credential-free in the direction that matters: the ingest
// path never needs a credential into an edge cluster, and a compromised edge
// site cannot influence what any namespace resolves to. The index key is
// derived locally with the same ns-<uid> rule Milo projects with, so the two
// directions cannot drift.
//
// Each kubeconfig context name is taken as the upstream cluster name, which for
// a Milo project control plane is the project name.
//
// TODO: replace the kubeconfig context list with Milo's multicluster-runtime
// provider, so project control planes are engaged and disengaged as projects
// come and go instead of at process start.
type UpstreamClusters struct {
	KubeconfigPath string
	ResyncPeriod   time.Duration
}

// Start builds an informer per kubeconfig context and blocks until ctx is done.
// It returns once every informer has been started; population is observable
// through [NamespaceIndex.HasSynced].
func (u UpstreamClusters) Start(ctx context.Context, index *NamespaceIndex) error {
	if u.KubeconfigPath == "" {
		return fmt.Errorf("no upstream kubeconfig configured")
	}

	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: u.KubeconfigPath}
	rawConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).RawConfig()
	if err != nil {
		return fmt.Errorf("failed loading upstream kubeconfig: %w", err)
	}

	if len(rawConfig.Contexts) == 0 {
		return fmt.Errorf("upstream kubeconfig %s defines no contexts", u.KubeconfigPath)
	}

	for contextName := range rawConfig.Contexts {
		if err := u.startCluster(ctx, index, rules, contextName); err != nil {
			return err
		}
	}

	return nil
}

func (u UpstreamClusters) startCluster(ctx context.Context, index *NamespaceIndex, rules *clientcmd.ClientConfigLoadingRules, clusterName string) error {
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{CurrentContext: clusterName},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed building client config for cluster %q: %w", clusterName, err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed building client for cluster %q: %w", clusterName, err)
	}

	factory := informers.NewSharedInformerFactory(client, u.ResyncPeriod)
	informer := factory.Core().V1().Namespaces().Informer()

	record := func(obj any) {
		namespace, ok := obj.(*corev1.Namespace)
		if !ok {
			return
		}
		index.Upsert(DownstreamNamespaceName(namespace.UID), UpstreamNamespaceRef{
			ClusterName: clusterName,
			Namespace:   namespace.Name,
		})
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { record(obj) },
		UpdateFunc: func(_, obj any) { record(obj) },
	}); err != nil {
		return fmt.Errorf("failed adding namespace handler for cluster %q: %w", clusterName, err)
	}

	index.AddSyncedFunc(informer.HasSynced)
	factory.Start(ctx.Done())

	klog.InfoS("Watching upstream namespaces", "cluster", clusterName)

	return nil
}
