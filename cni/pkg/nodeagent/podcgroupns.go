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
	"io/fs"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type PodToNetns map[string]WorkloadInfo

func (p PodToNetns) Close() {
	for _, wl := range p {
		wl.Netns.Close()
	}
}

type PodNetnsFinder interface {
	FindNetnsForPods(filter map[types.UID]*corev1.Pod) (PodToNetns, error)
}

type PodNetnsProcFinder struct {
	proc fs.FS
}

func NewPodNetnsProcFinder(proc fs.FS) *PodNetnsProcFinder {
	return &PodNetnsProcFinder{proc: proc}
}

func isNotNumber(r rune) bool {
	return r < '0' || r > '9'
}

type PodNetnsEntry struct {
	uid     types.UID
	netns   fs.File
	netnsfd uintptr
	inode   uint64
}
