# Sub-issue 7: Envoy Deep Inspection Tools

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement advanced Envoy proxy inspection tools that go beyond the standard proxy-config commands, including real-time stats analysis, log level management, access log parsing, Envoy response flag interpretation, and detailed filter chain analysis.

## Motivation

Envoy provides a wealth of debugging information through its admin API that istioctl only partially exposes. For advanced troubleshooting, the agent needs access to Envoy stats (connection counts, error rates, circuit breaker state), the ability to adjust log levels dynamically, and intelligence to interpret Envoy response flags and access log patterns.

## Detailed Design

### 1. Envoy Stats Tool

**EnvoyStatsTool** — wraps `istioctl x envoy-stats`
```go
type EnvoyStatsInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Type      string `json:"type,omitempty" jsonschema:"enum=server,clusters,description=Stats type (default: server)"`
    Filter    string `json:"filter,omitempty" jsonschema:"description=Filter stats by pattern (e.g., cluster.outbound|9080||reviews.default)"`
}
type EnvoyStatsOutput struct {
    Stats   map[string]interface{} `json:"stats"`
    Summary string                 `json:"summary"`
}
```
- **When to use**: Check circuit breaker status, connection pool health, upstream error rates
- **Data source**: Envoy admin API `/stats?format=json` or `/clusters?format=json`

**Key stats to surface**:
- `cluster.*.upstream_cx_active` — Active connections to upstream
- `cluster.*.upstream_cx_connect_fail` — Connection failures
- `cluster.*.upstream_rq_timeout` — Request timeouts
- `cluster.*.outlier_detection.*` — Outlier detection events
- `cluster.*.circuit_breakers.*` — Circuit breaker state
- `cluster.*.ssl.connection_error` — TLS errors
- `listener.*.downstream_cx_active` — Active downstream connections
- `server.live` — Whether the proxy is live

### 2. Envoy Log Level Tool

**EnvoyLogLevelTool** — wraps `istioctl proxy-config log`
```go
type EnvoyLogLevelInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Level     string `json:"level,omitempty" jsonschema:"enum=trace,debug,info,warning,error,critical,off,description=Set all loggers to this level"`
    Loggers   map[string]string `json:"loggers,omitempty" jsonschema:"description=Set specific loggers (e.g., {\"http\": \"debug\", \"connection\": \"trace\"})"`
    Reset     bool   `json:"reset,omitempty" jsonschema:"description=Reset to default log levels"`
}
type EnvoyLogLevelOutput struct {
    CurrentLevels map[string]string `json:"current_levels"`
    Summary       string            `json:"summary"`
}
```
- **When to use**: Temporarily increase logging for debugging, then reset
- **Mutating**: YES — requires user confirmation (changes proxy behavior)
- **Data source**: Envoy admin API POST `/logging`

**Key Envoy loggers**:
- `http` — HTTP connection manager
- `connection` — TCP connections
- `upstream` — Upstream connection management
- `router` — HTTP routing decisions
- `rbac` — RBAC/authorization decisions
- `jwt` — JWT authentication
- `lua` — Lua filter
- `wasm` — WASM filter

### 3. Envoy Response Flag Interpreter

**ResponseFlagTool** — interprets Envoy response flags from access logs
```go
type ResponseFlagInput struct {
    Flags string `json:"flags" jsonschema:"required,description=Envoy response flags string (e.g., NR, UF, URX, DC)"`
}
type ResponseFlagOutput struct {
    Flags       []FlagExplanation `json:"flags"`
    Suggestions []string          `json:"suggestions"`
    Summary     string            `json:"summary"`
}
type FlagExplanation struct {
    Flag        string `json:"flag"`
    Meaning     string `json:"meaning"`
    CommonCause string `json:"common_cause"`
}
```

**Supported flags and their meanings**:
| Flag | Meaning | Common Cause |
|---|---|---|
| `NR` | No route configured | Missing VirtualService, wrong hostname |
| `UF` | Upstream connection failure | Destination unreachable, crash |
| `UH` | No healthy upstream | All endpoints unhealthy |
| `UO` | Upstream overflow (circuit breaker) | Circuit breaker triggered |
| `URX` | Upstream retry limit exceeded | Too many retries |
| `DC` | Downstream connection termination | Client disconnected |
| `LH` | Local service failed health check | Service not ready |
| `UT` | Upstream request timeout | Slow upstream |
| `LR` | Connection local reset | Local connection error |
| `RL` | Rate limited | Rate limit policy hit |
| `UAEX` | Unauthorized external service | ExtAuthz denied |
| `RLSE` | Rate limit service error | Rate limit service unreachable |
| `IH` | Incompatible HTTP version | Protocol mismatch |
| `SI` | Stream idle timeout | No data transferred |
| `DPE` | Downstream protocol error | Invalid HTTP from client |

- **When to use**: User reports specific response codes or pastes access log lines
- **No cluster access needed**: Pure interpretation tool

### 4. Access Log Parser Tool

**AccessLogParserTool** — parses and interprets Envoy access logs
```go
type AccessLogParserInput struct {
    LogLine string `json:"log_line" jsonschema:"required,description=Raw Envoy access log line to parse"`
}
type AccessLogParserOutput struct {
    Timestamp         string `json:"timestamp"`
    Method            string `json:"method"`
    Path              string `json:"path"`
    Protocol          string `json:"protocol"`
    ResponseCode      int    `json:"response_code"`
    ResponseFlags     string `json:"response_flags"`
    BytesReceived     int    `json:"bytes_received"`
    BytesSent         int    `json:"bytes_sent"`
    Duration          int    `json:"duration_ms"`
    UpstreamService   string `json:"upstream_service"`
    UpstreamHost      string `json:"upstream_host"`
    UpstreamCluster   string `json:"upstream_cluster"`
    DownstreamLocal   string `json:"downstream_local"`
    DownstreamRemote  string `json:"downstream_remote"`
    RequestedServer   string `json:"requested_server"`
    Authority         string `json:"authority"`
    FlagExplanation   []FlagExplanation `json:"flag_explanation,omitempty"`
    Suggestions       []string          `json:"suggestions"`
    Summary           string            `json:"summary"`
}
```
- **When to use**: User pastes an access log line and wants to understand it
- **Parses**: Istio's default access log format (`[%START_TIME%] "%REQ(:METHOD)%" ...`)

### 5. Envoy Config Dump Analysis Tool

**EnvoyConfigAnalysisTool** — deep analysis of Envoy configuration
```go
type EnvoyConfigAnalysisInput struct {
    PodName    string `json:"pod_name" jsonschema:"required"`
    Namespace  string `json:"namespace" jsonschema:"required"`
    Focus      string `json:"focus,omitempty" jsonschema:"enum=listeners,clusters,routes,secrets,all,description=Which config area to analyze"`
    TargetFQDN string `json:"target_fqdn,omitempty" jsonschema:"description=Analyze config for a specific destination FQDN"`
}
type EnvoyConfigAnalysisOutput struct {
    Issues      []ConfigIssue `json:"issues,omitempty"`
    ConfigPath  string        `json:"config_path,omitempty"` // The path traffic would take
    Summary     string        `json:"summary"`
}
type ConfigIssue struct {
    Severity    string `json:"severity"` // Error, Warning, Info
    Component   string `json:"component"` // listener, cluster, route, secret
    Description string `json:"description"`
    Suggestion  string `json:"suggestion"`
}
```
- **When to use**: Deep dive into Envoy configuration for a specific traffic path
- **Analysis includes**:
  - Filter chain matching for a specific destination
  - Route matching verification
  - Cluster health and circuit breaker configuration
  - TLS context verification
  - Missing or misconfigured filter chains

### 6. Envoy Bootstrap Info Tool

**EnvoyBootstrapTool** — wraps `istioctl proxy-config bootstrap`
```go
type EnvoyBootstrapInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type EnvoyBootstrapOutput struct {
    IstioVersion  string            `json:"istio_version"`
    EnvoyVersion  string            `json:"envoy_version"`
    MeshID        string            `json:"mesh_id"`
    ClusterID     string            `json:"cluster_id"`
    NodeMetadata  map[string]string `json:"node_metadata"`
    DiscoveryAddr string            `json:"discovery_address"`
    Summary       string            `json:"summary"`
}
```
- **When to use**: Check proxy version, control plane connectivity, node metadata

## Implementation Steps

1. [ ] Implement `EnvoyStatsTool` with key metrics extraction
2. [ ] Implement `EnvoyLogLevelTool` with confirmation flow
3. [ ] Implement `ResponseFlagTool` with comprehensive flag database
4. [ ] Implement `AccessLogParserTool` for Istio's default log format
5. [ ] Implement `EnvoyConfigAnalysisTool` for deep config analysis
6. [ ] Implement `EnvoyBootstrapTool`
7. [ ] Create Envoy response flag reference data
8. [ ] Create access log format parser
9. [ ] Write unit tests
10. [ ] Write integration tests with mock Envoy admin responses

## Acceptance Criteria

- [ ] Stats tool surfaces circuit breaker, connection pool, and error metrics
- [ ] Log level tool requires confirmation before changing levels
- [ ] Response flag interpreter covers all documented Envoy flags
- [ ] Access log parser handles Istio's default format and common variations
- [ ] Config analysis tool can trace a traffic path through listeners → routes → clusters → endpoints
- [ ] All tools work with both sidecar and gateway proxies
