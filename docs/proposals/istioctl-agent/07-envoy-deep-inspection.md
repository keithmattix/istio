# Sub-issue 7: Envoy Proxy Inspection & Deep Analysis Tools

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement Envoy proxy inspection tools that work on **any Envoy-based proxy** — sidecars, waypoint proxies (ambient mode), and ingress/egress gateways. These tools wrap `istioctl proxy-config` and Envoy admin API capabilities. Since waypoint proxies are Envoy instances with listeners, clusters, routes, and endpoints, all of these tools are shared across data plane modes.

Also includes advanced Envoy inspection tools: real-time stats analysis, log level management, access log parsing, Envoy response flag interpretation, and config drift detection.

## Motivation

Envoy is the common data plane across all Istio deployment modes. Whether debugging a sidecar, a waypoint proxy, or an ingress gateway, the same Envoy admin API and XDS concepts apply:
- **Clusters** represent upstream service groups
- **Listeners** handle incoming connections
- **Routes** match requests to clusters
- **Endpoints** are the actual backend IPs
- **Secrets** hold mTLS certificates

The key differences between proxy types are in the _content_ of these resources, not the _tools_ used to inspect them:
- **Sidecars** have outbound listeners (VirtualOutbound on 15001) + inbound listeners (on 15006)
- **Waypoints** have ONLY inbound listeners (HBONE termination on 15008 + internal)
- **Gateways** have listeners for their configured ports

## Detailed Design

### Standard Proxy-Config Tools (All Proxy Types)

#### ProxyConfigClusterTool — wraps `istioctl proxy-config cluster`
```go
type ProxyConfigClusterInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod (sidecar, waypoint, or gateway)"`
    Namespace string `json:"namespace" jsonschema:"required"`
    FQDN      string `json:"fqdn,omitempty" jsonschema:"description=Filter by service FQDN"`
    Port      int    `json:"port,omitempty" jsonschema:"description=Filter by port"`
    Direction string `json:"direction,omitempty" jsonschema:"enum=inbound,outbound"`
}
type ProxyConfigClusterOutput struct {
    ProxyType string         `json:"proxy_type"` // sidecar, waypoint, gateway
    Clusters  []EnvoyCluster `json:"clusters"`
    Summary   string         `json:"summary"`
}
type EnvoyCluster struct {
    Name            string `json:"name"`
    FQDN            string `json:"fqdn"`
    Port            int    `json:"port"`
    Subset          string `json:"subset,omitempty"`
    Direction       string `json:"direction"`
    Type            string `json:"type"` // EDS, STATIC, STRICT_DNS, etc.
    DestinationRule string `json:"destination_rule,omitempty"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Check if Envoy knows about a destination service, verify DestinationRule application
- **Data source**: Envoy admin API `config_dump?mask=dynamic_active_clusters,...`
- **Sidecar vs Waypoint**: Sidecars have outbound clusters for all visible services; waypoints have inbound clusters for services they manage

#### ProxyConfigListenerTool — wraps `istioctl proxy-config listener`
```go
type ProxyConfigListenerInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Port      int    `json:"port,omitempty"`
    Type      string `json:"type,omitempty" jsonschema:"enum=HTTP,TCP,HTTP+TCP"`
    Address   string `json:"address,omitempty"`
}
type ProxyConfigListenerOutput struct {
    ProxyType string          `json:"proxy_type"`
    Listeners []EnvoyListener `json:"listeners"`
    Summary   string          `json:"summary"`
}
type EnvoyListener struct {
    Name     string `json:"name"`
    Address  string `json:"address"`
    Port     int    `json:"port"`
    Protocol string `json:"protocol"` // HTTP, TCP, HTTP+TCP
    Chains   int    `json:"filter_chain_count"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Check if traffic is being intercepted/handled on the right ports
- **Sidecar**: Has VirtualOutbound (15001), VirtualInbound (15006)
- **Waypoint**: Has HBONE connect-terminate (15008), internal inbound listeners
- **Gateway**: Has listeners for configured ports (80, 443, etc.)

#### ProxyConfigRouteTool — wraps `istioctl proxy-config route`
```go
type ProxyConfigRouteInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
    RouteName string `json:"route_name,omitempty"`
}
type ProxyConfigRouteOutput struct {
    ProxyType string       `json:"proxy_type"`
    Routes    []EnvoyRoute `json:"routes"`
    Summary   string       `json:"summary"`
}
type EnvoyRoute struct {
    Name           string `json:"name"`
    VirtualHost    string `json:"virtual_host"`
    Domains        string `json:"domains"`
    Match          string `json:"match"`
    VirtualService string `json:"virtual_service,omitempty"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Debug routing decisions, verify VirtualService application
- **Waypoint context**: Shows how the waypoint routes traffic to backend services

#### ProxyConfigEndpointTool — wraps `istioctl proxy-config endpoint`
```go
type ProxyConfigEndpointInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Cluster   string `json:"cluster,omitempty" jsonschema:"description=Filter by cluster name"`
    Status    string `json:"status,omitempty" jsonschema:"enum=HEALTHY,UNHEALTHY,UNKNOWN"`
}
type ProxyConfigEndpointOutput struct {
    ProxyType string          `json:"proxy_type"`
    Endpoints []EnvoyEndpoint `json:"endpoints"`
    Summary   string          `json:"summary"`
}
type EnvoyEndpoint struct {
    Cluster string `json:"cluster"`
    Address string `json:"address"`
    Port    int    `json:"port"`
    Status  string `json:"status"` // HEALTHY, UNHEALTHY, UNKNOWN
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Check endpoint health, verify service discovery

#### ProxyConfigSecretTool — wraps `istioctl proxy-config secret`
```go
type ProxyConfigSecretInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type ProxyConfigSecretOutput struct {
    ProxyType string        `json:"proxy_type"`
    Secrets   []EnvoySecret `json:"secrets"`
    Summary   string        `json:"summary"`
}
type EnvoySecret struct {
    Name       string    `json:"name"`
    Type       string    `json:"type"` // Cert, Root CA, Validation Context
    ValidFrom  time.Time `json:"valid_from"`
    ValidUntil time.Time `json:"valid_until"`
    SerialNum  string    `json:"serial_number"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Debug mTLS certificate issues, check cert expiration

### XDS Sync & Comparison Tools (All Proxy Types)

#### ProxyConfigDiffTool — wraps `istioctl proxy-status <pod>`
```go
type ProxyConfigDiffInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type ProxyConfigDiffOutput struct {
    ProxyType    string `json:"proxy_type"`
    ClusterDiff  string `json:"cluster_diff,omitempty"`
    ListenerDiff string `json:"listener_diff,omitempty"`
    RouteDiff    string `json:"route_diff,omitempty"`
    InSync       bool   `json:"in_sync"`
    Summary      string `json:"summary"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Detect config drift between what istiod sent and what Envoy has
- **Wraps**: `istioctl/pkg/writer/compare/` comparator

### Advanced Envoy Inspection Tools

#### EnvoyStatsTool — wraps `istioctl x envoy-stats`
```go
type EnvoyStatsInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Type      string `json:"type,omitempty" jsonschema:"enum=server,clusters,description=Stats type (default: server)"`
    Filter    string `json:"filter,omitempty" jsonschema:"description=Filter stats by pattern"`
}
type EnvoyStatsOutput struct {
    ProxyType string                 `json:"proxy_type"`
    Stats     map[string]interface{} `json:"stats"`
    Summary   string                 `json:"summary"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
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

#### EnvoyLogLevelTool — wraps `istioctl proxy-config log`
```go
type EnvoyLogLevelInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Level     string `json:"level,omitempty" jsonschema:"enum=trace,debug,info,warning,error,critical,off"`
    Loggers   map[string]string `json:"loggers,omitempty" jsonschema:"description=Set specific loggers"`
    Reset     bool   `json:"reset,omitempty" jsonschema:"description=Reset to default log levels"`
}
type EnvoyLogLevelOutput struct {
    CurrentLevels map[string]string `json:"current_levels"`
    Summary       string            `json:"summary"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **Mutating**: YES — requires user confirmation
- **When to use**: Temporarily increase logging for debugging, then reset

**Key Envoy loggers**: `http`, `connection`, `upstream`, `router`, `rbac`, `jwt`, `lua`, `wasm`

#### EnvoyBootstrapTool — wraps `istioctl proxy-config bootstrap`
```go
type EnvoyBootstrapInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type EnvoyBootstrapOutput struct {
    ProxyType     string            `json:"proxy_type"`
    IstioVersion  string            `json:"istio_version"`
    EnvoyVersion  string            `json:"envoy_version"`
    MeshID        string            `json:"mesh_id"`
    ClusterID     string            `json:"cluster_id"`
    NodeMetadata  map[string]string `json:"node_metadata"`
    DiscoveryAddr string            `json:"discovery_address"`
    Summary       string            `json:"summary"`
}
```
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Check proxy version, control plane connectivity, node metadata

### Offline Analysis Tools (No Cluster Access Required)

#### ResponseFlagTool — interprets Envoy response flags from access logs
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

#### AccessLogParserTool — parses and interprets Envoy access logs
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
- **Parses**: Istio's default access log format from any Envoy proxy (sidecar, waypoint, or gateway)

#### EnvoyConfigAnalysisTool — deep analysis of Envoy configuration
```go
type EnvoyConfigAnalysisInput struct {
    PodName    string `json:"pod_name" jsonschema:"required,description=Any Envoy proxy pod"`
    Namespace  string `json:"namespace" jsonschema:"required"`
    Focus      string `json:"focus,omitempty" jsonschema:"enum=listeners,clusters,routes,secrets,all"`
    TargetFQDN string `json:"target_fqdn,omitempty" jsonschema:"description=Analyze config for a specific destination FQDN"`
}
type EnvoyConfigAnalysisOutput struct {
    ProxyType   string        `json:"proxy_type"`
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
- **Applies to**: Sidecar, Waypoint, Gateway
- **When to use**: Deep dive into Envoy configuration for a specific traffic path
- **Analysis includes**: Filter chain matching, route verification, cluster health, TLS context, missing filter chains

### Cross-Mode Troubleshooting Workflows

These workflows apply regardless of data plane mode since they use shared Envoy inspection tools:

#### Workflow: "503 errors from my service"
1. `ClusterModeTool` — Detect if source/destination are sidecar or ambient
2. `ProxyStatusTool` — Check if proxy is in sync with istiod
3. `ProxyConfigClusterTool` — Check destination cluster exists (on sidecar or waypoint)
4. `ProxyConfigEndpointTool` — Check endpoints are HEALTHY
5. `ProxyConfigRouteTool` — Check route matches correctly
6. `ProxyConfigSecretTool` — Check if mTLS certs are valid
7. `KubernetesPodLogsTool(container=istio-proxy)` — Check proxy logs for errors
8. If mTLS issue: Check PeerAuthentication and DestinationRule TLS settings

#### Workflow: "mTLS handshake failure"
1. `ProxyConfigSecretTool` on both source and destination proxies — Check certs
2. `DescribePodTool` — Check PeerAuthentication mode
3. `AuthzCheckTool` — Check AuthorizationPolicies (works on both sidecar and waypoint)
4. `BoundaryCheck(CertManager)` — Check if using external CA
5. Guide: STRICT mode requires both sides to have valid mTLS certs

## Implementation Steps

1. [ ] Implement `ProxyConfigClusterTool` (all proxy types)
2. [ ] Implement `ProxyConfigListenerTool` (all proxy types)
3. [ ] Implement `ProxyConfigRouteTool` (all proxy types)
4. [ ] Implement `ProxyConfigEndpointTool` (all proxy types)
5. [ ] Implement `ProxyConfigSecretTool` (all proxy types)
6. [ ] Implement `ProxyConfigDiffTool` (all proxy types)
7. [ ] Implement `EnvoyStatsTool` (all proxy types)
8. [ ] Implement `EnvoyLogLevelTool` with confirmation flow (all proxy types)
9. [ ] Implement `EnvoyBootstrapTool` (all proxy types)
10. [ ] Implement `ResponseFlagTool` (offline)
11. [ ] Implement `AccessLogParserTool` (offline)
12. [ ] Implement `EnvoyConfigAnalysisTool` (all proxy types)
13. [ ] Encode cross-mode troubleshooting workflows in system prompt
14. [ ] Write unit tests with mock Envoy config dumps for sidecar, waypoint, and gateway
15. [ ] Write integration tests

## Acceptance Criteria

- [ ] All proxy-config tools work on sidecar, waypoint, AND gateway proxies
- [ ] Tools detect proxy type and include it in output for context
- [ ] Stats tool surfaces circuit breaker, connection pool, and error metrics
- [ ] Log level tool requires confirmation before changing levels
- [ ] Response flag interpreter covers all documented Envoy flags
- [ ] Access log parser handles Istio's default format from any proxy type
- [ ] Config analysis tool can trace a traffic path through listeners → routes → clusters → endpoints
- [ ] Config diff tool detects drift for any proxy type
- [ ] Output acknowledges differences (e.g., "waypoint only has inbound listeners" is not flagged as an issue)
