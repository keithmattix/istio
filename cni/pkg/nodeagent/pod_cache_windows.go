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

package nodeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/Microsoft/hcsshim/hcn"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/zdsapi"
	corev1 "k8s.io/api/core/v1"
)

// Hold a cache of node local pods with their netns
// if we don't know the netns, the pod will still be here with a nil netns.
type podNetnsCache struct {
	getNamespaceDetails func(nspath string) (NamespaceCloser, error)

	currentPodCache map[string]WorkloadInfo
	mu              sync.RWMutex
}

var _ PodNetnsCache = &podNetnsCache{}

var errNoCompartment = fmt.Errorf("no compartment found for namespace")

type workloadInfo struct {
	workload  *zdsapi.WorkloadInfo
	namespace NamespaceCloser
}

type dataWithID struct {
	ID string `json:"id"`
}

func newPodNetnsCache(getNamespaceDetails func(namespaceGuid string) (NamespaceCloser, error)) *podNetnsCache {
	return &podNetnsCache{
		getNamespaceDetails: getNamespaceDetails,
		currentPodCache:     map[string]WorkloadInfo{},
	}
}
func (wi workloadInfo) Workload() *zdsapi.WorkloadInfo {
	return wi.workload
}

func (wi workloadInfo) NetnsCloser() NetnsCloser {
	return wi.namespace
}

func (p *podNetnsCache) ReadCurrentPodSnapshot() map[string]WorkloadInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// snapshot the cache to avoid long locking
	return maps.Clone(p.currentPodCache)
}

func (p *podNetnsCache) GetEndpointsForNamespaceID(id uint32) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, info := range p.currentPodCache {
		namespaceCloser, ok := info.NetnsCloser().(NamespaceCloser)
		if !ok {
			return nil, fmt.Errorf("pod cache entry is not a NamespaceCloser")
		}
		if namespaceCloser.Namespace().ID == id {
			return namespaceCloser.Namespace().EndpointIds, nil
		}
	}
	return nil, fmt.Errorf("no namespace found in cache for id %d", id)

}

func (p *podNetnsCache) UpsertPodCache(pod *corev1.Pod, namespace string) (NamespaceCloser, error) {
	newnetns, err := p.getNamespaceDetails(namespace)

	if errors.Is(err, errNoCompartment) {
		// This is a pre-WS2025 pod, so we need to watch it in the queue
		log.Infof("")
	} else if err != nil {
		return nil, err
	}
	wl := workloadInfo{
		workload:  podToWorkload(pod),
		namespace: newnetns,
	}
	return p.UpsertPodCacheWithNetns(string(pod.UID), wl), nil
}

// Update the cache with the given Netns. If there is already a Netns for the given uid, we return it, and close the one provided.
func (p *podNetnsCache) UpsertPodCacheWithNetns(uid string, workload WorkloadInfo) NamespaceCloser {
	// lock current snapshot pod map
	workloadNetns := workload.NetnsCloser().(NamespaceCloser)
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing := p.currentPodCache[uid]; existing.NetnsCloser() != nil {
		// TODO: find some way to fail gracefully?
		existingNetns := existing.NetnsCloser().(NamespaceCloser)
		if existingNetns != nil {
			// If the guids match
			if existingNetns.Namespace().GUID == workloadNetns.Namespace().GUID {
				// Replace the workload, but keep the old GUID (doesn't really matter)
				p.currentPodCache[uid] = workloadInfo{
					workload:  workload.Workload(),
					namespace: existingNetns,
				}
				// already in cache
				return existingNetns
			}
			log.Debug("namespace guid mismatch, using the new one")
		}
	}

	// Doesn't exist yet, add it
	p.currentPodCache[uid] = workload
	return workloadNetns
}

// Get the netns if it's in the cache
func (p *podNetnsCache) Get(uid string) NamespaceCloser {
	// lock current snapshot pod map
	p.mu.RLock()
	defer p.mu.RUnlock()
	if info, f := p.currentPodCache[uid]; f {
		nc, _ := info.NetnsCloser().(NamespaceCloser)
		return nc // Don't panic because this could be nil
	}
	return nil
}

func getNamespaceDetailsFromRoot() func(namespaceGuid string) (NamespaceCloser, error) {
	return func(namespaceGuid string) (NamespaceCloser, error) {
		ns, err := hcn.GetNamespaceByID(namespaceGuid)
		if err != nil {
			return nil, err
		}

		if ns.NamespaceId == 0 {
			// This namespace doesn't have a compartment yet.
			// This is probably pre-WS2025, so we need to watch this pod
			// in the queue and push it to ztunnel once a compartment is created
			return nil, errNoCompartment
		}
		log.Infof("found namespace id %d for guid %s", ns.NamespaceId, namespaceGuid)

		endpointIDs, err := getNamespaceEndpoints(ns.Resources)
		if err != nil {
			return nil, fmt.Errorf("could not get namespace endpoints: %v", err)
		}
		return &namespaceCloser{
			ns: WindowsNamespace{
				ID:          ns.NamespaceId,
				GUID:        ns.Id,
				EndpointIds: endpointIDs,
			},
		}, nil
	}
}

func getNamespaceEndpoints(resources []hcn.NamespaceResource) ([]string, error) {
	var endpointIDs []string
	for _, r := range resources {
		if r.Type != hcn.NamespaceResourceTypeEndpoint {
			continue
		}
		var d dataWithID
		if err := json.Unmarshal(r.Data, &d); err != nil {
			log.Debugf("could not unmarshal endpoint data: %v", err)
			continue
		}
		endpointIDs = append(endpointIDs, d.ID)
	}
	return endpointIDs, nil
}
