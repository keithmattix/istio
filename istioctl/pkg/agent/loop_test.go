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

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"istio.io/istio/istioctl/pkg/agent/providers"
	"istio.io/istio/istioctl/pkg/agent/tools"
)

type scriptedProvider struct {
	responses []string
	idx       int
}

func (s *scriptedProvider) Generate(context.Context, []providers.Message, []providers.ToolSpec) (string, error) {
	if s.idx >= len(s.responses) {
		return "", nil
	}
	resp := s.responses[s.idx]
	s.idx++
	return resp, nil
}

func TestLoopDryRun(t *testing.T) {
	registry := tools.NewToolRegistry()
	_ = registry.Register(tools.Tool{Name: "analyze", Description: "Analyze"})
	loop := NewLoop(Config{Provider: "ollama", MaxIterations: 1, DryRun: true}, &scriptedProvider{}, registry)

	got, err := loop.Ask(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" {
		t.Fatal("expected dry-run output")
	}
}

func TestLoopCallsToolThenReturnsAnswer(t *testing.T) {
	provider := &scriptedProvider{responses: []string{"tool: analyze", "done"}}
	registry := tools.NewToolRegistry()
	_ = registry.Register(tools.Tool{
		Name:        "analyze",
		Description: "Analyze",
		Execute: func(context.Context, json.RawMessage) (tools.ToolResult, error) {
			return tools.ToolResult{Summary: "analysis result"}, nil
		},
	})
	loop := NewLoop(Config{Provider: "ollama", MaxIterations: 3}, provider, registry)

	got, err := loop.Ask(context.Background(), "what is wrong?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "done" {
		t.Fatalf("unexpected answer: %q", got)
	}
}
