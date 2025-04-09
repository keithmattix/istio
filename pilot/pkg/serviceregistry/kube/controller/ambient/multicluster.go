// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ambient

import (
	"net/netip"
	"strings"

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	istiomulticluster "istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

const ClusterKRTMetadataKey = "cluster"

func (a *index) buildGlobalCollections(
	LocalCluster *Cluster,
	LocalAuthzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	LocalPeerAuths krt.Collection[*securityclient.PeerAuthentication],
	LocalGatewayClasses krt.Collection[*v1beta1.GatewayClass],
	LocalWorkloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	LocalServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	options Options,
	opts krt.OptionsBuilder,
) error {
	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	clusters := buildRemoteClustersCollection(
		options,
		opts,
		istiomulticluster.DefaultBuildClientsFromConfig,
		filter,
	)
	a.remoteClusters = clusters

	LocalPods := LocalCluster.pods
	LocalServices := LocalCluster.services
	LocalNamespaces := LocalCluster.namespaces
	LocalNodes := LocalCluster.nodes
	LocalEndpointSlices := LocalCluster.endpointSlices
	LocalGateways := LocalCluster.gateways
	LocalWaypoints := a.WaypointsCollection(options.ClusterID, LocalGateways, LocalGatewayClasses, LocalPods, opts)

	GlobalEndpointSlices := krt.NewManyFromNothing(nestedCollectionFromLocalAndRemote(LocalEndpointSlices, clusters, func(c Cluster) krt.Collection[*discovery.EndpointSlice] {
		return c.endpointSlices
	}, opts))
	endpointSliceInformersByCluster := informerIndexByCluster(GlobalEndpointSlices)

	LocalMeshConfig := options.MeshConfig
	// These first collections can't be merged since the Kubernetes APIs don't have enough room
	// for e.g. duplicate IPs, etc. So we keep around collections of collections and indexes per cluster.
	GlobalPods := krt.NewManyFromNothing(nestedCollectionFromLocalAndRemote(LocalPods, clusters, func(c Cluster) krt.Collection[*v1.Pod] {
		return c.pods
	}, opts))
	// Pod informers indexable by cluster ID
	podInformersByCluster := informerIndexByCluster(GlobalPods)

	GlobalServices := krt.NewManyFromNothing(nestedCollectionFromLocalAndRemote(LocalServices, clusters, func(c Cluster) krt.Collection[*v1.Service] {
		return c.services
	}, opts))
	serviceInformersByCluster := informerIndexByCluster(GlobalServices)

	GlobalGateways := krt.NewManyFromNothing(nestedCollectionFromLocalAndRemote(LocalGateways, clusters, func(c Cluster) krt.Collection[*v1beta1.Gateway] {
		return c.gateways
	}, opts))
	gatewayInformersByCluster := informerIndexByCluster(GlobalGateways)
	GlobalGatewaysWithCluster := krt.NewCollection(clusters, collectionFromCluster[*v1beta1.Gateway]("GatewaysWithCluster", GlobalGateways, gatewayInformersByCluster, opts))
	globalGatewaysByCluster := nestedCollectionIndexByCluster(GlobalGatewaysWithCluster)

	GlobalNamespaces := krt.NewManyFromNothing(nestedCollectionFromLocalAndRemote(LocalNamespaces, clusters, func(c Cluster) krt.Collection[*v1.Namespace] {
		return c.namespaces
	}, opts))
	namespaceInformersByCluster := informerIndexByCluster(GlobalNamespaces)

	GlobalNodes := krt.NewManyFromNothing(nestedCollectionFromLocalAndRemote(LocalNodes, clusters, func(c Cluster) krt.Collection[*v1.Node] {
		return c.nodes
	}, opts))
	nodeInformersByCluster := informerIndexByCluster(GlobalNodes)

	globalNodes := krt.NewCollection(clusters, collectionFromCluster[*v1.Node]("Nodes", GlobalNodes, nodeInformersByCluster, opts))
	// Set up collections for remote clusters
	GlobalNetworks := buildGlobalNetworkCollections(
		clusters,
		LocalNamespaces,
		GlobalGatewaysWithCluster,
		globalGatewaysByCluster,
		options,
		opts,
	)
	a.networks = GlobalNetworks
	GlobalWaypoints := GlobalWaypointsCollection(
		LocalWaypoints,
		clusters,
		LocalGatewayClasses,
		GlobalGateways,
		gatewayInformersByCluster,
		GlobalPods,
		podInformersByCluster,
		GlobalNetworks,
		opts,
	)

	WaypointsByCluster := nestedCollectionIndexByCluster(GlobalWaypoints)
	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := PolicyCollections(LocalAuthzPolicies, LocalPeerAuths, LocalMeshConfig, LocalWaypoints, opts, a.Flags)
	AllPolicies.RegisterBatch(PushXds(a.XDSUpdater,
		func(i model.WorkloadAuthorization) model.ConfigKey {
			if i.Authorization == nil {
				return model.ConfigKey{} // nop, filter this out
			}
			return model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace}
		}), false)

	LocalWorkloadServices := a.ServicesCollection(LocalCluster.ID, LocalCluster.services, LocalServiceEntries, LocalWaypoints, LocalNamespaces, opts)
	// Now we get to collections where we can actually merge duplicate keys, so we can use nested collections
	GlobalWorkloadServicesWithCluster := GlobalMergedWorkloadServicesCollection(
		LocalCluster,
		LocalWorkloadServices,
		LocalWaypoints,
		clusters,
		LocalServiceEntries,
		GlobalServices,
		serviceInformersByCluster,
		GlobalWaypoints,
		WaypointsByCluster,
		GlobalNamespaces,
		namespaceInformersByCluster,
		GlobalNetworks,
		options.DomainSuffix,
		opts,
	)

	GobalWorkloadServicesWithClusterByCluster := nestedCollectionIndexByCluster(GlobalWorkloadServicesWithCluster)

	GlobalMergedWorkloadServicesWithCluster := krt.NestedJoinWithMergeCollection(
		GlobalWorkloadServicesWithCluster,
		mergeServiceInfosWithCluster(LocalCluster.ID),
		opts.WithName("GlobalMergedServiceInfosWithCluster")...,
	)

	GlobalMergedWorkloadServices := krt.MapCollection(GlobalMergedWorkloadServicesWithCluster, unwrapObjectWithCluster, opts.WithName("GlobalMergedServiceInfos")...)

	ServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](GlobalMergedWorkloadServices, networkAddressFromService)
	ServiceInfosByOwningWaypointHostname := krt.NewIndex(GlobalMergedWorkloadServices, func(s model.ServiceInfo) []NamespaceHostname {
		// Filter out waypoint services
		// TODO: we are looking at the *selector* -- we should be looking the labels themselves or something equivalent.
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Service.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetHostname()
		if waypointAddress == nil {
			return nil
		}

		return []NamespaceHostname{{
			Namespace: waypointAddress.Namespace,
			Hostname:  waypointAddress.Hostname,
		}}
	})
	ServiceInfosByOwningWaypointIP := krt.NewIndex(GlobalMergedWorkloadServices, func(s model.ServiceInfo) []networkAddress {
		// Filter out waypoint services
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Service.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}
		netip, _ := netip.AddrFromSlice(waypointAddress.Address)
		netaddr := networkAddress{
			network: waypointAddress.Network,
			ip:      netip.String(),
		}

		return []networkAddress{netaddr}
	})
	GlobalMergedWorkloadServices.RegisterBatch(krt.BatchedEventFilter(
		func(a model.ServiceInfo) *workloadapi.Service {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Service
		},
		PushXdsAddress(a.XDSUpdater, model.ServiceInfo.ResourceName),
	), false)

	LocalNamespacesInfo := krt.NewCollection(LocalNamespaces, func(ctx krt.HandlerContext, ns *v1.Namespace) *model.NamespaceInfo {
		return &model.NamespaceInfo{
			Name:               ns.Name,
			IngressUseWaypoint: strings.EqualFold(ns.Labels["istio.io/ingress-use-waypoint"], "true"),
		}
	}, opts.WithName("LocalNamespacesInfo")...)

	LocalNodeLocality := NodesCollection(
		LocalNodes,
		opts.WithName("LocalNodeLocality")...,
	)
	GlobalNodeLocality := GlobalNodesCollection(globalNodes, opts.Stop(), opts.WithName("GlobalNodeLocality")...)
	GlobalNodeLocalityByCluster := nestedCollectionIndexByCluster(GlobalNodeLocality)

	GlobalWorkloads := MergedGlobalWorkloadsCollection(
		LocalCluster,
		LocalWaypoints,
		LocalNodeLocality,
		clusters,
		LocalWorkloadEntries,
		LocalServiceEntries,
		GlobalPods,
		podInformersByCluster,
		GlobalNodeLocality,
		GlobalNodeLocalityByCluster,
		options.MeshConfig,
		AuthorizationPolicies,
		LocalPeerAuths,
		GlobalWaypoints,
		WaypointsByCluster,
		LocalWorkloadServices,
		GlobalWorkloadServicesWithCluster,
		GobalWorkloadServicesWithClusterByCluster,
		GlobalEndpointSlices,
		endpointSliceInformersByCluster,
		GlobalNamespaces,
		namespaceInformersByCluster,
		GlobalNetworks,
		options.ClusterID,
		options.Flags,
		options.DomainSuffix,
		opts,
	)

	WorkloadAddressIndex := krt.NewIndex[networkAddress, model.WorkloadInfo](GlobalWorkloads, networkAddressFromWorkload)
	WorkloadServiceIndex := krt.NewIndex[string, model.WorkloadInfo](GlobalWorkloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Workload.Services)
	})
	WorkloadWaypointIndexHostname := krt.NewIndex(GlobalWorkloads, func(w model.WorkloadInfo) []NamespaceHostname {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := w.Workload.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetHostname()
		if waypointAddress == nil {
			return nil
		}

		return []NamespaceHostname{{
			Namespace: waypointAddress.Namespace,
			Hostname:  waypointAddress.Hostname,
		}}
	})
	WorkloadWaypointIndexIP := krt.NewIndex(GlobalWorkloads, func(w model.WorkloadInfo) []networkAddress {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := w.Workload.Waypoint
		if waypoint == nil {
			return nil
		}

		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}
		netip, _ := netip.AddrFromSlice(waypointAddress.Address)
		netaddr := networkAddress{
			network: waypointAddress.Network,
			ip:      netip.String(),
		}

		return []networkAddress{netaddr}
	})
	GlobalWorkloads.RegisterBatch(krt.BatchedEventFilter(
		func(a model.WorkloadInfo) *workloadapi.Workload {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Workload
		},
		PushXdsAddress(a.XDSUpdater, model.WorkloadInfo.ResourceName),
	), false)

	if features.EnableIngressWaypointRouting {
		RegisterEdsShim(
			a.XDSUpdater,
			GlobalWorkloads,
			LocalNamespacesInfo,
			WorkloadServiceIndex,
			GlobalMergedWorkloadServices,
			ServiceAddressIndex,
			opts,
		)
	}
	a.namespaces = LocalNamespacesInfo

	a.workloads = workloadsCollection{
		Collection:               GlobalWorkloads,
		ByAddress:                WorkloadAddressIndex,
		ByServiceKey:             WorkloadServiceIndex,
		ByOwningWaypointHostname: WorkloadWaypointIndexHostname,
		ByOwningWaypointIP:       WorkloadWaypointIndexIP,
	}

	a.services = servicesCollection{
		Collection:               GlobalMergedWorkloadServices,
		ByAddress:                ServiceAddressIndex,
		ByOwningWaypointHostname: ServiceInfosByOwningWaypointHostname,
		ByOwningWaypointIP:       ServiceInfosByOwningWaypointIP,
	}
	a.authorizationPolicies = AllPolicies
	// TODO: Should this be global?
	a.waypoints = waypointsCollection{
		Collection: LocalWaypoints,
	}

	return nil
}

func nestedCollectionFromLocalAndRemote[T controllers.ComparableObject](
	localCollection krt.Collection[T],
	clustersCollection krt.Collection[Cluster],
	remoteCollectionGetter func(c Cluster) krt.Collection[T],
	_ krt.OptionsBuilder,
) krt.TransformationEmptyToMulti[krt.Collection[T]] {
	return func(ctx krt.HandlerContext) []krt.Collection[T] {
		allCollections := []krt.Collection[T]{localCollection}
		syncers := []krt.Syncer{localCollection}
		clusters := krt.Fetch(ctx, clustersCollection)
		for _, c := range clusters {
			remoteCollection := remoteCollectionGetter(c)
			if remoteCollection == nil {
				log.Warnf("Cluster %s has no pod informer", c.ID)
				continue
			}
			allCollections = append(allCollections, remoteCollection)
			syncers = append(syncers, remoteCollection)
		}

		return allCollections
	}
}

func collectionFromCluster[T controllers.ComparableObject](
	name string,
	informerCollection krt.Collection[krt.Collection[T]],
	informerIndex krt.Index[cluster.ID, krt.Collection[T]],
	opts krt.OptionsBuilder,
) krt.TransformationSingle[Cluster, krt.Collection[config.ObjectWithCluster[T]]] {
	return func(ctx krt.HandlerContext, c Cluster) *krt.Collection[config.ObjectWithCluster[T]] {
		clientPtr := krt.FetchOne(ctx, informerCollection, krt.FilterIndex(informerIndex, c.ID))
		if clientPtr == nil {
			log.Warnf("Cluster %s has no pod informer", c.ID)
			return nil
		}
		client := *clientPtr
		objWithCluster := krt.NewCollection(client, func(ctx krt.HandlerContext, obj T) *config.ObjectWithCluster[T] {
			return &config.ObjectWithCluster[T]{ClusterID: c.ID, Object: &obj}
		}, opts.With(
			krt.WithName(name+"WithCluster"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...)
		return &objWithCluster
	}
}

func informerIndexByCluster[T controllers.ComparableObject](
	informerCollection krt.Collection[krt.Collection[T]],
) krt.Index[cluster.ID, krt.Collection[T]] {
	return krt.NewIndex[cluster.ID, krt.Collection[T]](informerCollection, func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			log.Warnf("Cluster metadata not set on collection %v", col)
			return nil
		}
		id, ok := val.(cluster.ID)
		if !ok {
			log.Warnf("Invalid cluster metadata set on collection %v: %v", col, val)
			return nil
		}
		return []cluster.ID{id}
	})
}

func nestedCollectionIndexByCluster[T any](
	collection krt.Collection[krt.Collection[T]],
) krt.Index[cluster.ID, krt.Collection[T]] {
	return krt.NewIndex[cluster.ID, krt.Collection[T]](collection, func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			log.Warnf("Cluster metadata not set on collection %v", col)
			return nil
		}
		id, ok := val.(cluster.ID)
		if !ok {
			log.Warnf("Invalid cluster metadata set on collection %v: %v", col, val)
			return nil
		}
		return []cluster.ID{id}
	})
}

func mergeServiceInfosWithCluster(localClusterID cluster.ID) func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
	return func(serviceInfos []config.ObjectWithCluster[model.ServiceInfo]) *config.ObjectWithCluster[model.ServiceInfo] {
		if len(serviceInfos) == 0 {
			return nil
		}
		if len(serviceInfos) == 1 {
			return &serviceInfos[0]
		}

		// TODO: this is inaccurate; VIPs need to be scoped
		var vips sets.Set[*workloadapi.NetworkAddress]
		var sans sets.Set[string]
		ports := map[string]sets.Set[*workloadapi.Port]{}
		var base *config.ObjectWithCluster[model.ServiceInfo]
		for _, obj := range serviceInfos {
			if obj.Object == nil {
				continue
			}
			ports[obj.Object.Source.String()] = sets.New(obj.Object.Service.Ports...)
			vips.InsertAll(obj.Object.Service.GetAddresses()...)
			sans.InsertAll(obj.Object.Service.GetSubjectAltNames()...)
			if obj.ClusterID == localClusterID {
				if obj.Object.Source.Kind == kind.ServiceEntry {
					// If there's a service entry that's considered external to the mesh, we
					// prioritize that
					base = &obj
				} else if base == nil {
					base = &obj
				}
			}
		}

		if base.Object == nil {
			// No local objects found, so just use the first one
			base = &serviceInfos[0]
		}

		basePorts := sets.New(base.Object.Service.Ports...)
		for source, portSet := range ports {
			if !portSet.Equals(basePorts) {
				log.Warnf("ServiceInfo derived from %s has mismatched ports. Base ports %v != %v", source, basePorts, portSet)
			}
		}

		// TODO: Do we need to merge anything else?
		base.Object.Service.Addresses = vips.UnsortedList()
		base.Object.Service.SubjectAltNames = sans.UnsortedList()

		// Rememeber, we have to re-precompute the serviceinfo since we changed it
		return &config.ObjectWithCluster[model.ServiceInfo]{
			ClusterID: base.ClusterID,
			Object:    precomputeServicePtr(base.Object),
		}
	}
}

func mergeWorkloadInfosWithCluster(localClusterID cluster.ID) func(workloadInfos []config.ObjectWithCluster[model.WorkloadInfo]) *config.ObjectWithCluster[model.WorkloadInfo] {
	return func(workloadInfos []config.ObjectWithCluster[model.WorkloadInfo]) *config.ObjectWithCluster[model.WorkloadInfo] {
		if len(workloadInfos) == 0 {
			return nil
		}
		if len(workloadInfos) == 1 {
			return &workloadInfos[0]
		}

		log.Warnf("Duplicate workloadinfos found for %v. Trying to take local and then the first if we can't find it", workloadInfos)
		for _, obj := range workloadInfos {
			if obj.Object == nil {
				continue
			}
			if obj.ClusterID == localClusterID {
				return &obj
			}
		}

		log.Warnf("No local workloadinfo found for %v. Taking the first", workloadInfos)
		return &workloadInfos[0]
	}
}

func unwrapObjectWithCluster[T any](obj config.ObjectWithCluster[T]) T {
	if obj.Object == nil {
		return ptr.Empty[T]()
	}
	return *obj.Object
}

func wrapObjectWithCluster[T any](clusterID cluster.ID) func(obj T) config.ObjectWithCluster[T] {
	return func(obj T) config.ObjectWithCluster[T] {
		return config.ObjectWithCluster[T]{ClusterID: clusterID, Object: &obj}
	}
}
