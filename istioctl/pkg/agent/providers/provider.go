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

package providers

import (
	"context"
	"fmt"
)

type Message struct {
	Role    string
	Content string
}

type ToolSpec struct {
	Name        string
	Description string
}

type Client interface {
	Generate(ctx context.Context, messages []Message, tools []ToolSpec) (string, error)
}

type Config struct {
	Provider    string
	Model       string
	APIKey      string
	APIEndpoint string
}

func NewClient(cfg Config) (Client, error) {
	switch cfg.Provider {
	case "openai":
		return NewOpenAI(cfg), nil
	case "ollama":
		return NewOllama(cfg), nil
	case "anthropic":
		return NewAnthropic(cfg), nil
	case "azure":
		return NewAzure(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}
