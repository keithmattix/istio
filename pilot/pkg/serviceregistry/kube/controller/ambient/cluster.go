package ambient

import (
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
)

var _ krt.ResourceNamer = Cluster{}

type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client         kube.Client
	ClusterDetails krt.Singleton[ClusterDetails]

	// TODO: Figure out if we really need this and how to use it in krt
	KubeConfigSha [sha256.Size]byte
	Filter        kubetypes.DynamicObjectFilter
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// initialSyncTimeout is set when RunAndWait timed out
	initialSyncTimeout *atomic.Bool
	stop               <-chan struct{}
	namespaces         krt.Collection[*corev1.Namespace]
	pods               krt.Collection[*corev1.Pod]
	services           krt.Collection[*corev1.Service]
	endpointSlices     krt.Collection[*discoveryv1.EndpointSlice]
	nodes              krt.Collection[*corev1.Node]
	gateways           krt.Collection[*v1beta1.Gateway]
}

type ClusterDetails struct {
	SystemNamespace string
	Network         network.ID
}

func (c Cluster) ResourceName() string {
	return c.ID.String()
}

func (c ClusterDetails) ResourceName() string {
	return c.SystemNamespace
}

func (c *Cluster) Run(debugger *krt.DebugHandler) {
	if features.RemoteClusterTimeout > 0 {
		time.AfterFunc(features.RemoteClusterTimeout, func() {
			if !c.initialSync.Load() {
				log.Errorf("remote cluster %s failed to sync after %v", c.ID, features.RemoteClusterTimeout)
				timeouts.With(clusterLabel.Value(string(c.ID))).Increment()
			}
			c.initialSyncTimeout.Store(true)
		})
	}

	opts := krt.NewOptionsBuilder(c.stop, fmt.Sprintf("ambient/cluster[%s]/", c.ID), debugger)
	filter := kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}
	// Register all of the informers before starting the client
	Namespaces := krt.NewInformer[*v1.Namespace](c.Client, opts.With(
		append(opts.WithName("informer/Namespaces"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)
	Pods := krt.NewInformerFiltered[*v1.Pod](c.Client, kclient.Filter{
		ObjectFilter:    c.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripPodUnusedFields,
	}, opts.With(
		append(opts.WithName("informer/Pods"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](c.Client, gvr.KubernetesGateway, kubetypes.StandardInformer, filter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, opts.With(
		krt.WithName("informer/Gateways"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: c.ID,
		}),
	)...)
	servicesClient := kclient.NewFiltered[*v1.Service](c.Client, filter)
	Services := krt.WrapClient[*v1.Service](servicesClient, opts.With(
		append(opts.WithName("informer/Services"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	Nodes := krt.NewInformerFiltered[*v1.Node](c.Client, kclient.Filter{
		ObjectFilter:    c.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripNodeUnusedFields,
	}, opts.With(
		append(opts.WithName("informer/Nodes"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	EndpointSlices := krt.NewInformerFiltered[*discovery.EndpointSlice](c.Client, kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}, opts.With(
		append(opts.WithName("informer/EndpointSlices"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)
	if !c.Client.RunAndWait(c.stop) {
		log.Warnf("remote cluster %s failed to sync", c.ID)
		return
	}

	c.namespaces = Namespaces
	c.gateways = Gateways
	c.services = Services
	c.nodes = Nodes
	c.endpointSlices = EndpointSlices
	c.pods = Pods

	c.initialSync.Store(true)
}

func (c *Cluster) HasSynced() bool {
	// It could happen when a wrong credential provide, this cluster has no chance to run.
	// In this case, the `initialSyncTimeout` will never be set
	// In order not block istiod start up, check close as well.
	if c.Closed() {
		return true
	}
	return c.initialSync.Load() || c.initialSyncTimeout.Load()
}

func (c *Cluster) Closed() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *Cluster) SyncDidTimeout() bool {
	return !c.initialSync.Load() && c.initialSyncTimeout.Load()
}
