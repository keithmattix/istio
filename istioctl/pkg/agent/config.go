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
	"fmt"
	"os"
	"strings"
)

const (
	envProvider = "ISTIOCTL_AI_PROVIDER"
)

type Config struct {
	Provider       string
	Model          string
	APIKey         string
	APIEndpoint    string
	NonInteractive string
	MaxIterations  int
	Verbose        bool
	DryRun         bool
	ContextPath    string
}

func (c Config) Validate() error {
	if c.MaxIterations <= 0 {
		return fmt.Errorf("max-iterations must be > 0")
	}
	if c.Provider == "" {
		return fmt.Errorf("provider must be set with --provider or %s", envProvider)
	}
	switch c.Provider {
	case "openai", "anthropic", "ollama", "azure":
		return nil
	default:
		return fmt.Errorf("unsupported provider %q", c.Provider)
	}
}

func (c Config) WithEnvDefaults() Config {
	if c.Provider == "" {
		c.Provider = strings.ToLower(strings.TrimSpace(os.Getenv(envProvider)))
	}
	if c.APIKey == "" {
		c.APIKey = apiKeyFromProviderEnv(c.Provider)
	}
	return c
}

func apiKeyFromProviderEnv(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "azure":
		return os.Getenv("AZURE_OPENAI_API_KEY")
	default:
		return ""
	}
}
