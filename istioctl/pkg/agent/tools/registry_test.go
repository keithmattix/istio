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

package tools

import (
	"testing"

	"istio.io/istio/istioctl/pkg/cli"
)

func TestRegisterTools(t *testing.T) {
	r := NewToolRegistry()
	if err := RegisterTools(r, cli.NewCLIContext(nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, found := r.Get("proxy-status"); !found {
		t.Fatal("expected proxy-status tool")
	}
	if _, found := r.Get("analyze"); !found {
		t.Fatal("expected analyze tool")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewToolRegistry()
	if err := r.Register(Tool{Name: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Register(Tool{Name: "x"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
