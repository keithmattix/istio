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

import "istio.io/istio/istioctl/pkg/agent/providers"

type Conversation struct {
	history []providers.Message
}

func NewConversation() *Conversation {
	return &Conversation{history: make([]providers.Message, 0, 8)}
}

func (c *Conversation) Add(role, content string) {
	c.history = append(c.history, providers.Message{Role: role, Content: content})
}

func (c *Conversation) Messages() []providers.Message {
	out := make([]providers.Message, len(c.history))
	copy(out, c.history)
	return out
}
