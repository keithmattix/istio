//go:build linux
// +build linux

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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
	"net"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/zdsapi"
)

type ztunnelConnection struct {
	u       *net.UnixConn
	updates chan UpdateRequest
}

type updateRequest struct {
	update []byte
	fd     *int

	resp chan updateResponse
}

func (ur updateRequest) Update() []byte {
	return ur.update
}

func (ur updateRequest) Fd() *int {
	return ur.fd
}

func (ur updateRequest) Resp() chan updateResponse {
	return ur.resp
}

func newZtunnelConnection(u net.Conn) ZtunnelConnection {
	unixConn := u.(*net.UnixConn)
	return &ztunnelConnection{u: unixConn, updates: make(chan UpdateRequest, 100)}
}

func (z *ztunnelConnection) Conn() net.Conn {
	return z.u
}

func (z *ztunnelConnection) Close() {
	z.u.Close()
}

func (z *ztunnelConnection) Updates() chan UpdateRequest {
	return z.updates
}

func (z *ztunnelConnection) SendMsgAndWaitForAck(msg *zdsapi.WorkloadRequest, fd *int) (*zdsapi.WorkloadResponse, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return z.SendDataAndWaitForAck(data, fd)
}

func (z *ztunnelConnection) SendDataAndWaitForAck(data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
	var rights []byte
	if fd != nil {
		rights = unix.UnixRights(*fd)
	}
	err := z.u.SetWriteDeadline(time.Now().Add(readWriteDeadline))
	if err != nil {
		return nil, err
	}

	_, _, err = z.u.WriteMsgUnix(data, rights, nil)
	if err != nil {
		return nil, err
	}

	// wait for ack
	return z.ReadMessage(readWriteDeadline)
}

func (z *ztunnelConnection) ReadMessage(timeout time.Duration) (*zdsapi.WorkloadResponse, error) {
	m, _, err := readProto[zdsapi.WorkloadResponse](z.u, timeout, nil)
	return m, err
}

func readProto[T any, PT interface {
	proto.Message
	*T
}](c net.Conn, timeout time.Duration, oob []byte) (PT, int, error) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return nil, -1, fmt.Errorf("couldn't convert %q to unixConn", c)
	}
	var buf [1024]byte
	err := c.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, 0, err
	}
	n, oobn, flags, _, err := u.ReadMsgUnix(buf[:], oob)
	if err != nil {
		return nil, 0, err
	}
	if flags&unix.MSG_TRUNC != 0 {
		return nil, 0, fmt.Errorf("truncated message")
	}
	if flags&unix.MSG_CTRUNC != 0 {
		return nil, 0, fmt.Errorf("truncated control message")
	}
	var resp T
	var respPtr PT = &resp
	err = proto.Unmarshal(buf[:n], respPtr)
	if err != nil {
		return nil, 0, err
	}
	return respPtr, oobn, nil
}

func (z *ztunnelConnection) Send(ctx context.Context, data []byte, fd *int) (*zdsapi.WorkloadResponse, error) {
	ret := make(chan updateResponse, 1)
	req := updateRequest{
		update: data,
		fd:     fd,
		resp:   ret,
	}
	select {
	case z.Updates() <- req:
	case <-ctx.Done():
		return nil, fmt.Errorf("context expired before request sent: %v", ctx.Err())
	}

	select {
	case r := <-ret:
		return r.resp, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("context expired before response received: %v", ctx.Err())
	}
}
