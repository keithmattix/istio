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
	"fmt"

	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/ptr"
)

// MapCollection is just a light facade on top of another collection
// that uses a map function to trasform T into K
type mapCollection[T any, U any] struct {
	collectionName string
	id             collectionUID
	collection     internalCollection[T]
	mapFunc        func(T) U
}

func (m *mapCollection[T, U]) GetKey(k string) *U {
	if obj := m.collection.GetKey(k); obj != nil {
		return ptr.Of(m.mapFunc(*obj))
	}
	return nil
}

func (m *mapCollection[T, U]) List() []U {
	var res []U
	for _, obj := range m.collection.List() {
		res = append(res, m.mapFunc(obj))
	}
	return res
}

func (m *mapCollection[T, U]) Register(handler func(Event[U])) HandlerRegistration {
	return m.collection.Register(func(t Event[T]) {
		e := Event[U]{
			Event: t.Event,
		}
		if t.Old != nil {
			e.Old = ptr.Of(m.mapFunc(*t.Old))
		}
		if t.New != nil {
			e.New = ptr.Of(m.mapFunc(*t.New))
		}
		handler(e)
	})
}

func (m *mapCollection[T, U]) RegisterBatch(handler func([]Event[U]), runExistingState bool) HandlerRegistration {
	return m.collection.RegisterBatch(func(t []Event[T]) {
		var events []Event[U]
		for _, t := range t {
			e := Event[U]{
				Event: t.Event,
			}
			if t.Old != nil {
				e.Old = ptr.Of(m.mapFunc(*t.Old))
			}
			if t.New != nil {
				e.New = ptr.Of(m.mapFunc(*t.New))
			}
			events = append(events, e)
		}
		handler(events)
	}, runExistingState)
}

func (m *mapCollection[T, U]) Metadata() Metadata {
	return m.collection.Metadata()
}

// nolint: unused // (not true, its to implement an interface)
func (m *mapCollection[T, U]) augment(a any) any {
	// not supported in this collection type
	return a
}

// nolint: unused // (not true, its to implement an interface)
func (m *mapCollection[T, U]) name() string { return m.collectionName }

// nolint: unused // (not true, its to implement an interface)
func (m *mapCollection[T, U]) uid() collectionUID { return m.id }

// nolint: unused // (not true, its to implement an interface)
func (m *mapCollection[T, U]) dump() CollectionDump {
	// Dump should not be used on join; instead its preferred to enroll each individual collection. Maybe reconsider
	// in the future if there is a need
	return CollectionDump{}
}

// nolint: unused // (not true, its to implement an interface)
func (m *mapCollection[T, U]) index(extract func(o U) []string) kclient.RawIndexer {
	t := func(o T) []string {
		return extract(m.mapFunc(o))
	}
	return m.collection.index(t)
}

func (m *mapCollection[T, U]) HasSynced() bool {
	return m.collection.HasSynced()
}

func (m *mapCollection[T, U]) WaitUntilSynced(stop <-chan struct{}) bool {
	return m.collection.WaitUntilSynced(stop)
}

func MapCollection[T, U any](
	collection Collection[T],
	mapFunc func(T) U,
	opts ...CollectionOption,
) Collection[U] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("Join[%v]", ptr.TypeName[T]())
	}
	return &mapCollection[T, U]{
		collectionName: o.name,
		id:             nextUID(),
		collection:     collection.(internalCollection[T]),
		mapFunc:        mapFunc,
	}
}
