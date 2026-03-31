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

import "context"

type ollamaClient struct {
	cfg Config
}

func NewOllama(cfg Config) Client {
	if cfg.Model == "" {
		cfg.Model = "llama3"
	}
	return &ollamaClient{cfg: cfg}
}

func (o *ollamaClient) Generate(_ context.Context, messages []Message, _ []ToolSpec) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	return "[ollama] " + messages[len(messages)-1].Content, nil
}
