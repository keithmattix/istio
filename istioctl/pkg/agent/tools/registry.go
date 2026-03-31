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
	"context"
	"encoding/json"
	"fmt"

	"istio.io/istio/istioctl/pkg/agent/providers"
	"istio.io/istio/istioctl/pkg/cli"
)

type ToolRegistry struct {
	tools map[string]Tool
	list  []Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[string]Tool{}, list: []Tool{}}
}

func (r *ToolRegistry) Register(tool Tool) error {
	if _, exists := r.tools[tool.Name]; exists {
		return fmt.Errorf("tool %q already registered", tool.Name)
	}
	r.tools[tool.Name] = tool
	r.list = append(r.list, tool)
	return nil
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, f := r.tools[name]
	return t, f
}

func (r *ToolRegistry) List() []Tool {
	out := make([]Tool, len(r.list))
	copy(out, r.list)
	return out
}

func (r *ToolRegistry) AsProviderTools() []providers.ToolSpec {
	out := make([]providers.ToolSpec, 0, len(r.list))
	for _, t := range r.list {
		out = append(out, providers.ToolSpec{Name: t.Name, Description: t.Description})
	}
	return out
}

func RegisterTools(registry *ToolRegistry, _ cli.Context) {
	for _, tool := range []Tool{
		{
			Name:        "proxy-status",
			Description: "Get Istio proxy synchronization status",
			Category:    "state",
			ReadOnly:    true,
			Modes:       []DataPlaneMode{DataPlaneModeBoth},
			Execute: func(context.Context, json.RawMessage) (ToolResult, error) {
				return ToolResult{Summary: "proxy-status not yet implemented"}, nil
			},
		},
		{
			Name:        "analyze",
			Description: "Analyze Istio configuration for issues",
			Category:    "diagnostic",
			ReadOnly:    true,
			Modes:       []DataPlaneMode{DataPlaneModeBoth},
			Execute: func(context.Context, json.RawMessage) (ToolResult, error) {
				return ToolResult{Summary: "analyze not yet implemented"}, nil
			},
		},
	} {
		_ = registry.Register(tool)
	}
}
