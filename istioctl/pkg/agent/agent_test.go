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
	"bytes"
	"context"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/agent/providers"
	"istio.io/istio/istioctl/pkg/cli"
)

type fakeProvider struct{}

func (f fakeProvider) Generate(_ context.Context, messages []providers.Message, _ []providers.ToolSpec) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	return "final: " + messages[len(messages)-1].Content, nil
}

type capturingProviderFactory struct {
	last providers.Config
}

func (c *capturingProviderFactory) newClient(cfg providers.Config) (providers.Client, error) {
	c.last = cfg
	return fakeProvider{}, nil
}

func TestCmdHasRequiredFlags(t *testing.T) {
	cmd := Cmd(cli.NewCLIContext(nil))
	for _, f := range []string{
		"provider",
		"model",
		"api-key",
		"api-endpoint",
		"non-interactive",
		"max-iterations",
		"verbose",
		"dry-run",
		"context",
	} {
		if cmd.Flags().Lookup(f) == nil {
			t.Fatalf("expected flag %q", f)
		}
	}
}

func TestRunNonInteractive(t *testing.T) {
	factory := &capturingProviderFactory{}
	oldFactory := newProviderClient
	newProviderClient = factory.newClient
	t.Cleanup(func() { newProviderClient = oldFactory })

	t.Setenv("ISTIOCTL_AI_PROVIDER", "ollama")

	var out bytes.Buffer
	err := run(context.Background(), cli.NewCLIContext(nil), &out, strings.NewReader(""), Config{
		NonInteractive: "why are requests failing",
		MaxIterations:  1,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "final: why are requests failing") {
		t.Fatalf("unexpected output: %s", got)
	}
	if factory.last.Provider != "ollama" {
		t.Fatalf("expected provider from env to be used, got %q", factory.last.Provider)
	}
}

func TestConfigWithEnvDefaults(t *testing.T) {
	t.Setenv("ISTIOCTL_AI_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := Config{}.WithEnvDefaults()
	if cfg.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", cfg.Provider)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("expected api key from env, got %q", cfg.APIKey)
	}
}

func TestConfigValidation(t *testing.T) {
	if err := (Config{Provider: "openai", MaxIterations: 1}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := (Config{Provider: "", MaxIterations: 1}).Validate(); err == nil {
		t.Fatal("expected error for missing provider")
	}
	if err := (Config{Provider: "openai", MaxIterations: 0}).Validate(); err == nil {
		t.Fatal("expected error for max iterations")
	}
	if err := (Config{Provider: "invalid", MaxIterations: 1}).Validate(); err == nil {
		t.Fatal("expected error for provider")
	}
}
