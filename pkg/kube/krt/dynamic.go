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

package krt

import (
	"sync"

	"istio.io/istio/pkg/kube/kclient"
)

type dynamicMultiSyncer struct {
	syncers map[collectionUID]Syncer
}

func (s *dynamicMultiSyncer) HasSynced() bool {
	for _, syncer := range s.syncers {
		if !syncer.HasSynced() {
			return false
		}
	}
	return true
}

func (s *dynamicMultiSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	for _, syncer := range s.syncers {
		if !syncer.WaitUntilSynced(stop) {
			return false
		}
	}
	return true
}

type dynamicJoinHandlerRegistration struct {
	dynamicMultiSyncer
	removes map[collectionUID]func()
	sync.RWMutex
}

func (hr *dynamicJoinHandlerRegistration) UnregisterHandler() {
	hr.RLock()
	removes := hr.removes
	hr.RUnlock()
	// Unregister all the handlers
	for _, remover := range removes {
		remover()
	}
}

type collectionMembershipEvent int

const (
	collectionMembershipEventAdd collectionMembershipEvent = iota
	collectionMembershipEventDelete
)

type collectionChangeEvent[T any] struct {
	eventType       collectionMembershipEvent
	collectionValue internalCollection[T]
}

// nolint: unused // (not true)
type dynamicJoinIndexer struct {
	indexers map[collectionUID]kclient.RawIndexer
	sync.RWMutex
}

// nolint: unused // (not true)
func (j *dynamicJoinIndexer) Lookup(key string) []any {
	var res []any
	first := true
	j.RLock()
	defer j.RUnlock() // keithmattix: we're probably fine to defer as long as we don't have nested dynamic indexers
	for _, i := range j.indexers {
		l := i.Lookup(key)
		if len(l) > 0 && first {
			// TODO: add option to merge slices
			// This is probably not going to be performant if we need
			// to do lots of merges. Benchmark and optimize later.
			// Optimization: re-use the first returned slice
			res = l
			first = false
		} else {
			res = append(res, l...)
		}
	}
	return res
}
