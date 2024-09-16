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
	"context"
	"fmt"
	"net/netip"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/pkg/kube"
)

var log = scopes.CNIAgent

type MeshDataplane interface {
	// called first, (even before Start()).
	ConstructInitialSnapshot(ambientPods []*corev1.Pod) error
	Start(ctx context.Context)

	//	IsPodInMesh(ctx context.Context, pod *metav1.ObjectMeta, netNs string) (bool, error)
	AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error
	RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error

	Stop()
}

type Server struct {
	ctx        context.Context
	kubeClient kube.Client

	handlers  K8sHandlers
	dataplane MeshDataplane

	isReady *atomic.Value

	cniServerStopFunc func()
}

func NewServer(ctx context.Context, ready *atomic.Value, pluginSocket string, args AmbientArgs) (*Server, error) {
	client, err := buildKubeClient(args.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing kube client: %w", err)
	}

	s := &Server{
		ctx:        ctx,
		kubeClient: client,
		isReady:    ready,
	}

	s.dataplane, err = initMeshDataplane(client, args)
	if err != nil {
		return nil, fmt.Errorf("error initializing mesh dataplane")
	}

	s.NotReady()
	s.handlers = setupHandlers(s.ctx, s.kubeClient, s.dataplane, args.SystemNamespace)

	cniServer := startCniPluginServer(ctx, pluginSocket, s.handlers, s.dataplane)
	err = cniServer.Start()
	if err != nil {
		return nil, fmt.Errorf("error starting cni server: %w", err)
	}
	s.cniServerStopFunc = cniServer.Stop

	return s, nil
}

func (s *Server) Ready() {
	s.isReady.Store(true)
}

func (s *Server) NotReady() {
	s.isReady.Store(false)
}

// buildKubeClient creates the kube client
func buildKubeClient(kubeConfig string) (kube.Client, error) {
	// Used by validation
	kubeRestConfig, err := kube.DefaultRestConfig(kubeConfig, "", func(config *rest.Config) {
		config.QPS = 80
		config.Burst = 160
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating kube config: %v", err)
	}

	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig), "")
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client: %v", err)
	}

	return client, nil
}

func (s *Server) Start() {
	log.Info("CNI ambient server starting")
	s.kubeClient.RunAndWait(s.ctx.Done())
	log.Info("CNI ambient server kubeclient started")
	pods := s.handlers.GetActiveAmbientPodSnapshot()
	err := s.dataplane.ConstructInitialSnapshot(pods)
	if err != nil {
		log.Warnf("failed to construct initial snapshot: %v", err)
	}

	log.Info("CNI ambient server marking ready")
	s.Ready()
	s.dataplane.Start(s.ctx)
	s.handlers.Start()
}

func (s *Server) Stop() {
	s.cniServerStopFunc()
	s.dataplane.Stop()
}
