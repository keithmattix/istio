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

package inject

import (
	"encoding/json"
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	corev1 "k8s.io/api/core/v1"
)

const waitForZtunnelCommand = `
while ($true) {
  $ztunnelPort = netstat -an | Select-String -Pattern ":15008"
  if ($ztunnelPort) {
    Write-Output "Ztunnel Port 15008 is open."
    break
  } else {
    Write-Output "Ztunnel Port 15008 is not open. Retrying..."
    Start-Sleep -Seconds 1
  }
}
`

func (wh *Webhook) injectWindows(ar *kube.AdmissionReview, path string) *kube.AdmissionResponse {
	log := log.WithLabels("path", path)
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		handleError(log, fmt.Sprintf("Could not unmarshal raw object: %v %s", err,
			string(req.Object.Raw)))
		return toAdmissionResponse(err)
	}

	// Managed fields is sometimes extremely large, leading to excessive CPU time on patch generation
	// It does not impact the injection output at all, so we can just remove it.
	pod.ManagedFields = nil

	// Deal with potential empty fields, e.g., when the pod is created by a deployment
	podName := potentialPodName(pod.ObjectMeta)
	if pod.ObjectMeta.Namespace == "" {
		pod.ObjectMeta.Namespace = req.Namespace
	}

	log = log.WithLabels("pod", pod.Namespace+"/"+podName)
	log.Infof("Process sidecar injection request")
	log.Debugf("Object: %v", string(req.Object.Raw))
	log.Debugf("OldObject: %v", string(req.OldObject.Raw))

	wh.mu.RLock()
	// TODO: Create a custom windows one if necessay
	if !injectRequired(IgnoredNamespaces.UnsortedList(), wh.Config, &pod.Spec, pod.ObjectMeta) {
		log.Infof("Skipping due to policy check")
		totalSkippedInjections.Increment()
		wh.mu.RUnlock()
		return &kube.AdmissionResponse{
			Allowed: true,
		}
	}
	wh.mu.RUnlock()

	// Inject an initcontainer at the very beginning of the list
	// This initcontainer will just wait for a ztunnel port to be open

	// The patch will be built relative to the initial pod, capture its current state
	originalPodSpec, err := json.Marshal(&pod)
	if err != nil {
		handleError(log, fmt.Sprintf("Windows initcontainer injection failed: %v", err))
		return toAdmissionResponse(err)
	}

	windowsInitContainer := corev1.Container{
		Name:            "wait-for-ztunnel",
		Image:           "mcr.microsoft.com/windows/servercore:ltsc2022",
		Command:         []string{waitForZtunnelCommand},
		ImagePullPolicy: corev1.PullIfNotPresent,
	}

	pod.Spec.InitContainers = append(pod.Spec.InitContainers, windowsInitContainer)
	patch, err := createPatch(&pod, originalPodSpec)
	if err != nil {
		handleError(log, fmt.Sprintf("Windows initcontainer injection failed: %v", err))
		return toAdmissionResponse(err)
	}

	log.Debugf("AdmissionResponse: patch=%v\n", string(patch))
	reviewResponse := kube.AdmissionResponse{
		Allowed: true,
		Patch:   patch,
		PatchType: func() *string {
			pt := "JSONPatch"
			return &pt
		}(),
	}
	return &reviewResponse
}
