# Sub-issue 1: Core Agent Framework & CLI Integration

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement the core agent framework in istioctl, including the CLI entry point, agent loop, LLM provider abstraction, tool registry, and conversation management. This is the foundational infrastructure that all other sub-issues build on.

## Detailed Design

### 1. CLI Entry Point

Register as an experimental command following the established pattern:

```
istioctl x agent [flags]
istioctl x agent --provider=openai --model=gpt-4
istioctl x agent --provider=anthropic --model=claude-3-sonnet
istioctl x agent --provider=ollama --model=llama3  # Local/air-gapped
istioctl x agent --non-interactive "why is my pod getting 503 errors?"
```

**Location**: `istioctl/pkg/agent/agent.go`  
**Registration**: `istioctl/cmd/root.go` → `experimentalCmd.AddCommand(agent.Cmd(ctx))`

**Flags**:
| Flag | Type | Default | Description |
|---|---|---|---|
| `--provider` | string | `""` (env: `ISTIOCTL_AI_PROVIDER`) | LLM provider (openai, anthropic, ollama, azure) |
| `--model` | string | provider default | Model name |
| `--api-key` | string | `""` (env: provider-specific) | API key (prefer env var) |
| `--api-endpoint` | string | provider default | Custom API endpoint (for Ollama, Azure, proxies) |
| `--non-interactive` | string | `""` | Single-question mode (no REPL) |
| `--max-iterations` | int | `10` | Max ReAct loop iterations per question |
| `--verbose` | bool | `false` | Show tool calls and reasoning |
| `--dry-run` | bool | `false` | Show what tools would be called without executing |
| `--context` | string | `""` | Path to context file with additional troubleshooting info |

### 2. Agent Framework Selection

**Recommended approach: LangChainGo + MCP Go SDK hybrid**

**Why LangChainGo** (`github.com/tmc/langchaingo`):
- Battle-tested ReAct agent loop with tool calling
- Multi-provider LLM support (OpenAI, Anthropic, Ollama, Azure, etc.)
- Function calling support for structured tool invocation
- Active Go SDK with idiomatic patterns
- Lightweight dependency footprint

**Why MCP Go SDK** (`github.com/modelcontextprotocol/go-sdk`):
- Standard protocol for tool registration and schema definition
- Automatic JSON Schema generation from Go structs
- Enables external MCP clients to connect to istioctl's tools
- Future-proof interoperability with any MCP-compatible agent

**Integration pattern**:
- MCP SDK defines the tool schemas and handles tool registration
- LangChainGo provides the agent loop, LLM abstraction, and ReAct reasoning
- A thin adapter layer converts MCP tool definitions into LangChainGo tools

### 3. Package Structure

```
istioctl/pkg/agent/
├── agent.go              # Cobra command, CLI entry point
├── config.go             # Agent configuration, provider setup
├── loop.go               # ReAct agent loop (wraps LangChainGo executor)
├── conversation.go       # Conversation state, history management
├── providers/
│   ├── provider.go       # LLM provider interface
│   ├── openai.go         # OpenAI provider
│   ├── anthropic.go      # Anthropic provider
│   └── ollama.go         # Ollama (local) provider
├── tools/
│   ├── registry.go       # Tool registry (MCP-based)
│   ├── tool.go           # Base tool interface
│   ├── cluster_state.go  # proxy-status, version tools
│   ├── config_analysis.go # analyze tool
│   ├── proxy_config.go   # proxy-config tools
│   ├── describe.go       # describe tool
│   ├── authz.go          # authz check tool
│   ├── ztunnel.go        # ztunnel-config tools
│   ├── waypoint.go       # waypoint tools
│   ├── kubernetes.go     # kubectl wrapper tools
│   ├── envoy.go          # Envoy admin API tools
│   └── istiod_debug.go   # internal-debug tools
├── operations/
│   ├── registry.go       # Guided operation registry
│   ├── operation.go      # Base operation interface
│   ├── waypoint_deploy.go # Waypoint deployment wizard
│   ├── mtls_migration.go # mTLS migration wizard
│   ├── ambient_migrate.go # Ambient migration wizard
│   └── traffic_mgmt.go   # Traffic management wizard
├── context/
│   ├── cluster.go        # Cluster context detection (mode, version, namespaces)
│   ├── mode.go           # Data plane mode detection
│   └── boundary.go       # Boundary detection (CNI, DNS, NetworkPolicy)
└── output/
    ├── formatter.go      # Output formatting for terminal
    ├── markdown.go       # Markdown output for non-interactive
    └── color.go          # Colored terminal output
```

### 4. Tool Registry Pattern

```go
// Tool interface (MCP-compatible)
type Tool struct {
    Name        string
    Description string
    Category    string           // "diagnostic", "config", "state", "operation"
    InputSchema interface{}      // Auto-generated from Go struct via MCP SDK
    Execute     func(ctx context.Context, input json.RawMessage) (ToolResult, error)
    ReadOnly    bool             // If false, requires user confirmation
    Modes       []DataPlaneMode  // Which modes this tool applies to (sidecar, ambient, both)
}

// Registration pattern
func RegisterTools(registry *ToolRegistry, cliCtx cli.Context) {
    registry.Register(NewProxyStatusTool(cliCtx))
    registry.Register(NewAnalyzeTool(cliCtx))
    registry.Register(NewProxyConfigClusterTool(cliCtx))
    // ... etc
}
```

### 5. Conversation Flow

```
User Input → Agent Loop → LLM (reason about tools) → Tool Selection →
  → Tool Execution → Result → LLM (interpret results) →
  → [More tools needed?] → Loop back or → Final Response to User
```

For guided operations:
```
User Request → Operation Detection → Cluster Examination →
  → Clarifying Questions → User Responses → Configuration Generation →
  → User Confirmation → Apply (with --skip-confirmation safety) → Status Report
```

### 6. System Prompt Engineering

The agent needs a carefully crafted system prompt that includes:
- Istio architecture overview (sidecar vs ambient)
- Available tools and when to use them
- Boundary awareness rules (when to suggest checking CNI, CoreDNS, etc.)
- Safety rules (always confirm before modifying cluster)
- Troubleshooting decision trees for common scenarios

This should be a Go template that gets populated with cluster context at startup.

## Implementation Steps

1. [ ] Add LangChainGo and MCP Go SDK dependencies to `go.mod`
2. [ ] Create `istioctl/pkg/agent/` package structure
3. [ ] Implement `agent.go` Cobra command with flags
4. [ ] Implement `config.go` with provider configuration
5. [ ] Implement `providers/` with at least OpenAI and Ollama support
6. [ ] Implement `tools/registry.go` with MCP-based tool registration
7. [ ] Implement `tools/tool.go` base interface
8. [ ] Implement `loop.go` ReAct agent loop
9. [ ] Implement `conversation.go` for state management
10. [ ] Implement `output/formatter.go` for terminal output
11. [ ] Register command in `istioctl/cmd/root.go`
12. [ ] Write unit tests for agent loop, tool registry, and providers
13. [ ] Write integration test with mock LLM

## Dependencies

- `github.com/tmc/langchaingo` — ReAct agent loop, LLM providers
- `github.com/modelcontextprotocol/go-sdk` — Tool schemas, MCP protocol
- No changes to istiod or control plane

## Acceptance Criteria

- [ ] `istioctl x agent` starts a REPL session with configured LLM provider
- [ ] `istioctl x agent --non-interactive "question"` returns a single answer
- [ ] Tool registry correctly registers all diagnostic tools
- [ ] Agent loop correctly calls tools and interprets results
- [ ] `--dry-run` mode shows tool calls without executing
- [ ] `--verbose` mode shows reasoning steps
- [ ] Works with at least OpenAI and Ollama providers
