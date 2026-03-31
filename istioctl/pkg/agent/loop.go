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
	"fmt"
	"strings"

	"istio.io/istio/istioctl/pkg/agent/providers"
	"istio.io/istio/istioctl/pkg/agent/tools"
)

type Loop struct {
	cfg          Config
	client       providers.Client
	registry     *tools.ToolRegistry
	conversation *Conversation
}

func NewLoop(cfg Config, client providers.Client, registry *tools.ToolRegistry) *Loop {
	return &Loop{
		cfg:          cfg,
		client:       client,
		registry:     registry,
		conversation: NewConversation(),
	}
}

func (l *Loop) Ask(ctx context.Context, question string) (string, error) {
	l.conversation.Add("user", question)
	toolNames := make([]string, 0, len(l.registry.List()))
	for _, t := range l.registry.List() {
		toolNames = append(toolNames, t.Name)
	}
	if l.cfg.DryRun {
		return fmt.Sprintf("dry-run: available tools: %s", strings.Join(toolNames, ", ")), nil
	}

	for i := 0; i < l.cfg.MaxIterations; i++ {
		response, err := l.client.Generate(ctx, l.conversation.Messages(), l.registry.AsProviderTools())
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(response, "tool:") {
			l.conversation.Add("assistant", response)
			return response, nil
		}

		toolName := strings.TrimSpace(strings.TrimPrefix(response, "tool:"))
		tool, ok := l.registry.Get(toolName)
		if !ok {
			return "", fmt.Errorf("unknown tool requested: %s", toolName)
		}
		result, err := tool.Execute(ctx, nil)
		if err != nil {
			return "", err
		}
		l.conversation.Add("tool", result.Summary)
	}
	return "", fmt.Errorf("max iterations (%d) reached without final answer", l.cfg.MaxIterations)
}
