# Sub-issue 8: Testing, Documentation & UX

**Labels**: `enhancement`, `area/istioctl`, `area/test`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement comprehensive testing, documentation, and UX polish for the istioctl troubleshooting agent. This includes unit tests, integration tests, mock LLM testing, system prompt documentation, user documentation, and terminal UX enhancements.

## Detailed Design

### 1. Testing Strategy

#### Unit Tests

**Tool tests** (`istioctl/pkg/agent/tools/*_test.go`):
- Each tool tested with mock cluster data
- Tests verify input validation, structured output, error handling
- Use `istioctl/pkg/cli/mock_client.go` patterns for mocking Kubernetes clients
- Test both sidecar and ambient mode scenarios

**Agent loop tests** (`istioctl/pkg/agent/loop_test.go`):
- Mock LLM that returns predefined tool calls
- Verify correct tool dispatch and result handling
- Test max iteration limits
- Test error recovery in the ReAct loop

**Operation tests** (`istioctl/pkg/agent/operations/*_test.go`):
- Each guided operation tested with mock answers
- Verify YAML generation correctness
- Verify confirmation prompts are shown for mutating operations
- Test conditional step logic

**Boundary detection tests** (`istioctl/pkg/agent/context/*_test.go`):
- Test each boundary check with mock cluster state
- Verify correct guidance messages
- Test edge cases (no NetworkPolicies, missing CNI, etc.)

#### Integration Tests

**End-to-end with mock LLM**:
- Set up a mock LLM server (HTTP endpoint returning predetermined responses)
- Run full agent conversation flows
- Verify tools are called correctly and results interpreted

**With real istioctl packages**:
- Use existing test patterns from `istioctl/pkg/*/` test files
- Call tool implementations with realistic config dump data
- Verify output matches expected format

#### Testing Infrastructure

```go
// Mock LLM for testing
type MockLLM struct {
    responses []string // Predetermined responses in order
    toolCalls []ToolCall // Expected tool calls
    current   int
}

// Mock tool for testing the agent loop
type MockTool struct {
    name   string
    result ToolResult
}

// Test helper that creates a full agent with mock dependencies
func NewTestAgent(t *testing.T, mockLLM *MockLLM, tools ...Tool) *Agent {
    // ...
}
```

### 2. System Prompt Design

The system prompt is critical for agent behavior. It should be thoroughly documented and version-controlled.

**Location**: `istioctl/pkg/agent/prompts/`

```
prompts/
├── system.go       # Main system prompt template
├── sidecar.go      # Sidecar-specific troubleshooting context
├── ambient.go      # Ambient-specific troubleshooting context
├── boundaries.go   # Boundary awareness rules
├── operations.go   # Guided operation triggers and descriptions
└── safety.go       # Safety rules (confirmation requirements, etc.)
```

**System prompt structure**:
1. **Role definition**: "You are an Istio service mesh troubleshooting assistant..."
2. **Architecture context**: Brief overview of sidecar vs ambient mode
3. **Available tools**: Descriptions and when to use each
4. **Boundary awareness**: Rules for detecting non-Istio issues
5. **Safety rules**: Always confirm before modifying, never expose secrets
6. **Troubleshooting decision trees**: Common symptom → tool sequence mappings
7. **Cluster context** (dynamic): Current mode, version, namespace info

### 3. User Documentation

**Location**: Add to istio.io documentation site (or `docs/` in repo)

#### Quick Start Guide
```markdown
# istioctl agent - AI-Powered Troubleshooting

## Prerequisites
- `istioctl` installed
- Kubernetes cluster with Istio
- LLM provider API key (OpenAI, Anthropic, or local Ollama)

## Quick Start

# Using OpenAI
export OPENAI_API_KEY=your-key
istioctl x agent

# Using local Ollama
istioctl x agent --provider=ollama --api-endpoint=http://localhost:11434

# Single question mode
istioctl x agent --non-interactive "why am I getting 503 errors from my reviews service?"
```

#### Usage Examples
- Troubleshooting 503 errors
- Debugging mTLS failures
- Setting up waypoints with the agent
- Migrating from sidecar to ambient mode
- Understanding authorization policy issues

### 4. Terminal UX

#### Conversation Display
```
$ istioctl x agent

🔍 Istio Troubleshooting Agent (experimental)
   Connected to cluster: my-cluster
   Istio version: 1.24.0
   Data plane: ambient (3 namespaces), sidecar (2 namespaces)

You: Why is traffic to my reviews service failing?

Agent: Let me investigate. I'll check the service status and configuration.
  🔧 Running proxy-status... ✓
  🔧 Running analyze -n default... ✓
  🔧 Running describe svc reviews -n default... ✓

Based on my analysis:

1. **Issue Found**: The reviews service has 0 healthy endpoints
   - All 3 pods are in CrashLoopBackOff state
   - This is causing HTTP 503 (No Healthy Upstream) errors

2. **Root Cause**: The pods are crashing because of an OOM kill
   - Container memory limit is 64Mi, but the app needs ~128Mi

3. **Suggested Fix**:
   ```yaml
   resources:
     limits:
       memory: 256Mi
   ```

Would you like me to help with anything else?

You: Can you also help me set up a waypoint for this namespace?

Agent: I'd be happy to help deploy a waypoint! Let me examine your cluster first.
  🔧 Checking namespace mode... ✓
  🔧 Listing services... ✓

[Guided operation starts]
...
```

#### Visual Elements
- `🔧` Tool execution indicator
- `✓` / `✗` Success/failure indicators
- `⚠️` Warning indicator
- Colored output for severity levels
- Spinner for long-running operations
- YAML syntax highlighting for generated configs

#### Non-Interactive Mode Output
```
$ istioctl x agent --non-interactive "check my mesh health" --output json
{
  "tools_called": ["proxy-status", "analyze", "version"],
  "findings": [
    {"severity": "warning", "message": "2 proxies out of sync with istiod"},
    {"severity": "error", "message": "IST0101: VirtualService references unknown host"}
  ],
  "summary": "Found 1 error and 1 warning..."
}
```

### 5. Configuration File Support

**Location**: `$HOME/.istioctl/agent.yaml`
```yaml
# Default LLM provider configuration
provider: openai
model: gpt-4
# api-key: use OPENAI_API_KEY env var instead

# Agent behavior
max-iterations: 10
verbose: false

# Custom system prompt additions
extra-context: |
  Our cluster uses Calico CNI and cert-manager for CA.
  We use namespace-per-team isolation.
```

### 6. Telemetry & Feedback

- **Opt-in usage telemetry**: Track which tools are used most, common failure patterns
- **Feedback mechanism**: `Was this helpful? (y/n)` after each response
- **Error reporting**: Structured error logs for debugging agent issues

## Implementation Steps

1. [ ] Create test infrastructure (MockLLM, MockTool, TestAgent helpers)
2. [ ] Write unit tests for all tools (one test file per tool)
3. [ ] Write unit tests for agent loop
4. [ ] Write unit tests for operations
5. [ ] Write unit tests for boundary detection
6. [ ] Write integration tests with mock LLM server
7. [ ] Design and implement system prompt templates
8. [ ] Implement terminal UX (spinner, colors, YAML highlighting)
9. [ ] Implement configuration file support
10. [ ] Write quick start documentation
11. [ ] Write usage examples documentation
12. [ ] Write contributing guide for adding new tools/operations
13. [ ] Implement feedback mechanism
14. [ ] Implement non-interactive JSON output mode

## Acceptance Criteria

- [ ] >80% unit test coverage for agent package
- [ ] Integration tests pass with mock LLM
- [ ] System prompt produces consistent, helpful behavior
- [ ] Terminal UX is clean and informative
- [ ] Documentation covers all major use cases
- [ ] Configuration file works correctly
- [ ] Non-interactive mode produces structured output
- [ ] New tools can be added with minimal boilerplate (documented in contributing guide)
