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
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/agent/output"
	"istio.io/istio/istioctl/pkg/agent/providers"
	"istio.io/istio/istioctl/pkg/agent/tools"
	"istio.io/istio/istioctl/pkg/cli"
)

var newProviderClient = providers.NewClient

func Cmd(cliCtx cli.Context) *cobra.Command {
	cfg := Config{MaxIterations: 10}
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Experimental AI troubleshooting agent",
		Long: `Experimental Istio troubleshooting agent.

Supports interactive and non-interactive usage:
  istioctl x agent --provider=openai --model=gpt-4
  istioctl x agent --provider=ollama --model=llama3
  istioctl x agent --non-interactive "why is my pod getting 503 errors?"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), cliCtx, cmd.OutOrStdout(), cmd.InOrStdin(), cfg)
		},
	}
	cmd.Flags().StringVar(&cfg.Provider, "provider", "", "LLM provider (openai, anthropic, ollama, azure)")
	cmd.Flags().StringVar(&cfg.Model, "model", "", "Model name")
	cmd.Flags().StringVar(&cfg.APIKey, "api-key", "", "API key (prefer environment variables)")
	cmd.Flags().StringVar(&cfg.APIEndpoint, "api-endpoint", "", "Custom API endpoint")
	cmd.Flags().StringVar(&cfg.NonInteractive, "non-interactive", "", "Single-question mode")
	cmd.Flags().IntVar(&cfg.MaxIterations, "max-iterations", 10, "Max tool-calling loop iterations per question")
	cmd.Flags().BoolVar(&cfg.Verbose, "verbose", false, "Show detailed reasoning and tool calls")
	cmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Show what tools would be called without executing")
	cmd.Flags().StringVar(&cfg.ContextPath, "context", "", "Path to additional troubleshooting context file")
	return cmd
}

func run(ctx context.Context, cliCtx cli.Context, out io.Writer, in io.Reader, cfg Config) error {
	cfg = cfg.WithEnvDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	client, err := newProviderClient(providers.Config{
		Provider:    cfg.Provider,
		Model:       cfg.Model,
		APIKey:      cfg.APIKey,
		APIEndpoint: cfg.APIEndpoint,
	})
	if err != nil {
		return err
	}

	registry := tools.NewToolRegistry()
	if err := tools.RegisterTools(registry, cliCtx); err != nil {
		return err
	}
	loop := NewLoop(cfg, client, registry)
	formatter := output.Formatter{Verbose: cfg.Verbose}

	if cfg.NonInteractive != "" {
		answer, err := loop.Ask(ctx, cfg.NonInteractive)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, formatter.FinalResponse(answer))
		return nil
	}

	scanner := bufio.NewScanner(in)
	for {
		_, _ = fmt.Fprint(out, "agent> ")
		if !scanner.Scan() {
			return nil
		}
		question := strings.TrimSpace(scanner.Text())
		if question == "" {
			continue
		}
		if question == "exit" || question == "quit" {
			return nil
		}
		answer, err := loop.Ask(ctx, question)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, formatter.FinalResponse(answer))
	}
}
