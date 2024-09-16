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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/zdsapi"
)

var (
	ztunnelKeepAliveCheckInterval = 5 * time.Second
	readWriteDeadline             = 5 * time.Second
)

var ztunnelConnected = monitoring.NewGauge("ztunnel_connected",
	"number of connections to ztunnel")

type ZtunnelServer interface {
	Run(ctx context.Context)
	PodDeleted(ctx context.Context, uid string) error
	PodAdded(ctx context.Context, pod *v1.Pod, netns Netns) error
	Close() error
}

/*
To clean up stale ztunnels

	we may need to ztunnel to send its (uid, bootid / boot time) to us
	so that we can remove stale entries when the ztunnel pod is deleted
	or when the ztunnel pod is restarted in the same pod (remove old entries when the same uid connects again, but with different boot id?)

	save a queue of what needs to be sent to the ztunnel pod and send it one by one when it connects.

	when a new ztunnel connects with different uid, only propagate deletes to older ztunnels.
*/

type connMgr struct {
	connectionSet map[ZtunnelConnection]struct{}
	latestConn    ZtunnelConnection
	mu            sync.Mutex
}

type UpdateRequest interface {
	Update() []byte
	Fd() *int

	Resp() chan updateResponse
}

type updateResponse struct {
	err  error
	resp *zdsapi.WorkloadResponse
}

type ZtunnelConnection interface {
	Close()
	SendMsgAndWaitForAck(*zdsapi.WorkloadRequest, *int) (*zdsapi.WorkloadResponse, error)
	SendDataAndWaitForAck([]byte, *int) (*zdsapi.WorkloadResponse, error)
	Send(context.Context, []byte, *int) (*zdsapi.WorkloadResponse, error)
	Conn() net.Conn
	Updates() chan UpdateRequest
	ReadMessage(time.Duration) (*zdsapi.WorkloadResponse, error)
}

func (c *connMgr) addConn(conn ZtunnelConnection) {
	log.Debug("ztunnel connected")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectionSet[conn] = struct{}{}
	c.latestConn = conn
	ztunnelConnected.RecordInt(int64(len(c.connectionSet)))
}

func (c *connMgr) LatestConn() ZtunnelConnection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latestConn
}

func (c *connMgr) deleteConn(conn ZtunnelConnection) {
	log.Debug("ztunnel disconnected")
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.connectionSet, conn)
	if c.latestConn == conn {
		c.latestConn = nil
	}
	ztunnelConnected.RecordInt(int64(len(c.connectionSet)))
}

// this is used in tests
// nolint: unused
func (c *connMgr) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.connectionSet)
}

type ztunnelServer struct {
	listener *net.UnixListener

	// connections to pod delivered map
	// add pod goes to newest connection
	// delete pod goes to all connections
	conns *connMgr
	pods  PodNetnsCache
}

var _ ZtunnelServer = &ztunnelServer{}

func newZtunnelServer(addr string, pods PodNetnsCache) (*ztunnelServer, error) {
	if addr == "" {
		return nil, fmt.Errorf("addr cannot be empty")
	}

	resolvedAddr, err := net.ResolveUnixAddr("unixpacket", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve unix addr: %w", err)
	}
	// remove potentially existing address
	// Remove unix socket before use, if one is leftover from previous CNI restart
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		return nil, fmt.Errorf("failed to remove unix://%s: %w", addr, err)
	}

	l, err := net.ListenUnix("unixpacket", resolvedAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen unix: %w", err)
	}

	return &ztunnelServer{
		listener: l,
		conns: &connMgr{
			connectionSet: map[ZtunnelConnection]struct{}{},
		},
		pods: pods,
	}, nil
}

func (z *ztunnelServer) Close() error {
	return z.listener.Close()
}

func (z *ztunnelServer) Run(ctx context.Context) {
	context.AfterFunc(ctx, func() { _ = z.Close() })

	// Allow at most 5 requests per second. This is still a ridiculous amount; at most we should have 2 ztunnels on our node,
	// and they will only connect once and persist.
	// However, if they do get in a state where they call us in a loop, we will quickly OOM
	limit := rate.NewLimiter(rate.Limit(5), 1)
	for {
		log.Debug("accepting conn")
		if err := limit.Wait(ctx); err != nil {
			log.Errorf("failed to wait for ztunnel connection: %v", err)
			return
		}
		conn, err := z.accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Debug("listener closed - returning")
				return
			}

			log.Errorf("failed to accept conn: %v", err)
			continue
		}
		log.Debug("connection accepted")
		go func() {
			log.Debug("handling conn")
			if err := z.handleConn(ctx, conn); err != nil {
				log.Errorf("failed to handle conn: %v", err)
			}
		}()
	}
}

// ZDS protocol is very simple, for every message sent, and ack is sent.
// the ack only has temporal correlation (i.e. it is the first and only ack msg after the message was sent)
// All this to say, that we want to make sure that message to ztunnel are sent from a single goroutine
// so we don't mix messages and acks.
// nolint: unparam
func (z *ztunnelServer) handleConn(ctx context.Context, conn ZtunnelConnection) error {
	defer conn.Close()

	context.AfterFunc(ctx, func() {
		log.Debug("context cancelled, closing ztunnel server")
		conn.Close()
	})

	// before doing anything, add the connection to the list of active connections
	z.conns.addConn(conn)
	defer z.conns.deleteConn(conn)

	// get hello message from ztunnel
	m, _, err := readProto[zdsapi.ZdsHello](conn.Conn(), readWriteDeadline, nil)
	if err != nil {
		return err
	}
	log.WithLabels("version", m.Version).Infof("received hello from ztunnel")
	log.Debug("sending snapshot to ztunnel")
	if err := z.sendSnapshot(ctx, conn); err != nil {
		return err
	}
	for {
		// listen for updates:
		select {
		case update, ok := <-conn.Updates():
			if !ok {
				log.Debug("update channel closed - returning")
				return nil
			}
			log.Debugf("got update to send to ztunnel")
			resp, err := conn.SendDataAndWaitForAck(update.Update(), update.Fd())
			if err != nil {
				log.Errorf("ztunnel acked error: err %v ackErr %s", err, resp.GetAck().GetError())
			}
			log.Debugf("ztunnel acked")
			// Safety: Resp is buffered, so this will not block
			update.Resp() <- updateResponse{
				err:  err,
				resp: resp,
			}

		case <-time.After(ztunnelKeepAliveCheckInterval):
			// do a short read, just to see if the connection to ztunnel is
			// still alive. As ztunnel shouldn't send anything unless we send
			// something first, we expect to get an os.ErrDeadlineExceeded error
			// here if the connection is still alive.
			// note that unlike tcp connections, reading is a good enough test here.
			_, err := conn.ReadMessage(time.Second / 100)
			switch {
			case !errors.Is(err, os.ErrDeadlineExceeded):
				log.Debugf("ztunnel keepalive failed: %v", err)
				if errors.Is(err, io.EOF) {
					log.Debug("ztunnel EOF")
					return nil
				}
				return err
			case err == nil:
				log.Warn("ztunnel protocol error, unexpected message")
				return fmt.Errorf("ztunnel protocol error, unexpected message")
			default:
				// we get here if error is deadline exceeded, which means ztunnel is alive.
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (z *ztunnelServer) PodDeleted(ctx context.Context, uid string) error {
	r := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Del{
			Del: &zdsapi.DelWorkload{
				Uid: uid,
			},
		},
	}
	data, err := proto.Marshal(r)
	if err != nil {
		return err
	}

	log.Debugf("sending delete pod to ztunnel: %s %v", uid, r)

	var delErr []error

	z.conns.mu.Lock()
	defer z.conns.mu.Unlock()
	for conn := range z.conns.connectionSet {
		_, err := conn.Send(ctx, data, nil)
		if err != nil {
			delErr = append(delErr, err)
		}
	}
	return errors.Join(delErr...)
}

func podToWorkload(pod *v1.Pod) *zdsapi.WorkloadInfo {
	namespace := pod.ObjectMeta.Namespace
	name := pod.ObjectMeta.Name
	svcAccount := pod.Spec.ServiceAccountName
	return &zdsapi.WorkloadInfo{
		Namespace:      namespace,
		Name:           name,
		ServiceAccount: svcAccount,
	}
}

func (z *ztunnelServer) PodAdded(ctx context.Context, pod *v1.Pod, netns Netns) error {
	latestConn := z.conns.LatestConn()
	if latestConn == nil {
		return fmt.Errorf("no ztunnel connection")
	}
	uid := string(pod.ObjectMeta.UID)

	add := &zdsapi.AddWorkload{
		WorkloadInfo: podToWorkload(pod),
		Uid:          uid,
	}
	r := &zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_Add{
			Add: add,
		},
	}
	log := log.WithLabels(
		"uid", add.Uid,
		"name", add.WorkloadInfo.Name,
		"namespace", add.WorkloadInfo.Namespace,
		"serviceAccount", add.WorkloadInfo.ServiceAccount,
	)

	log.Infof("sending pod add to ztunnel")
	data, err := proto.Marshal(r)
	if err != nil {
		return err
	}

	fd := int(netns.Fd())
	resp, err := latestConn.Send(ctx, data, &fd)
	if err != nil {
		return err
	}

	if resp.GetAck().GetError() != "" {
		log.Errorf("failed to add workload: %s", resp.GetAck().GetError())
		return fmt.Errorf("got ack error: %s", resp.GetAck().GetError())
	}
	return nil
}

// TODO ctx is unused here
// nolint: unparam
func (z *ztunnelServer) sendSnapshot(ctx context.Context, conn ZtunnelConnection) error {
	snap := z.pods.ReadCurrentPodSnapshot()
	for uid, wl := range snap {
		var resp *zdsapi.WorkloadResponse
		var err error
		log := log.WithLabels("uid", uid)
		if wl.Workload != nil {
			log = log.WithLabels(
				"name", wl.Workload.Name,
				"namespace", wl.Workload.Namespace,
				"serviceAccount", wl.Workload.ServiceAccount)
		}
		if wl.Netns != nil {
			fd := int(wl.Netns.Fd())
			log.Infof("sending pod to ztunnel as part of snapshot")
			resp, err = conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
				Payload: &zdsapi.WorkloadRequest_Add{
					Add: &zdsapi.AddWorkload{
						Uid:          uid,
						WorkloadInfo: wl.Workload,
					},
				},
			}, &fd)
		} else {
			log.Infof("netns is not available for pod, sending 'keep' to ztunnel")
			resp, err = conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
				Payload: &zdsapi.WorkloadRequest_Keep{
					Keep: &zdsapi.KeepWorkload{
						Uid: uid,
					},
				},
			}, nil)
		}
		if err != nil {
			return err
		}
		if resp.GetAck().GetError() != "" {
			log.Errorf("add-workload: got ack error: %s", resp.GetAck().GetError())
		}
	}
	resp, err := conn.SendMsgAndWaitForAck(&zdsapi.WorkloadRequest{
		Payload: &zdsapi.WorkloadRequest_SnapshotSent{
			SnapshotSent: &zdsapi.SnapshotSent{},
		},
	}, nil)
	if err != nil {
		return err
	}
	log.Debugf("snaptshot sent to ztunnel")
	if resp.GetAck().GetError() != "" {
		log.Errorf("snap-sent: got ack error: %s", resp.GetAck().GetError())
	}

	return nil
}

func (z *ztunnelServer) accept() (ZtunnelConnection, error) {
	log.Debug("accepting unix conn")
	conn, err := z.listener.AcceptUnix()
	if err != nil {
		return nil, fmt.Errorf("failed to accept unix: %w", err)
	}
	log.Debug("accepted conn")
	return newZtunnelConnection(conn), nil
}
