# Sub-issue 2: Diagnostic Tool Wrappers

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Wrap existing istioctl diagnostic capabilities as agent-callable tools. Each tool should have well-defined input schemas, structured output, and clear descriptions that help the LLM understand when and how to use them.

## Detailed Design

### Tool Categories

#### 1. Cluster State Tools

**ProxyStatusTool** — wraps `istioctl proxy-status`
```go
type ProxyStatusInput struct {
    PodName   string `json:"pod_name,omitempty" jsonschema:"description=Specific pod to check sync status for"`
    Namespace string `json:"namespace,omitempty" jsonschema:"description=Namespace to filter by"`
}
type ProxyStatusOutput struct {
    Proxies []ProxySyncStatus `json:"proxies"`
    Summary string            `json:"summary"` // Human-readable summary
}
```
- **When to use**: Check if all proxies are in sync with the control plane
- **Wraps**: `istioctl/pkg/proxystatus/proxystatus.go` via XDS query to istiod
- **Data source**: XDS DiscoveryRequest to istiod with `syncz` debug type

**VersionTool** — wraps `istioctl version`
```go
type VersionInput struct{}
type VersionOutput struct {
    ClientVersion    string `json:"client_version"`
    ControlPlane     []ControlPlaneVersion `json:"control_plane"`
    DataPlaneProxies []ProxyVersion `json:"data_plane_proxies,omitempty"`
}
```
- **When to use**: Check version compatibility, detect version skew

#### 2. Configuration Analysis Tool

**AnalyzeTool** — wraps `istioctl analyze`
```go
type AnalyzeInput struct {
    Namespace     string   `json:"namespace,omitempty" jsonschema:"description=Namespace to analyze (empty for all)"`
    AllNamespaces bool     `json:"all_namespaces,omitempty" jsonschema:"description=Analyze all namespaces"`
    Analyzers     []string `json:"analyzers,omitempty" jsonschema:"description=Specific analyzer names to run"`
}
type AnalyzeOutput struct {
    Messages []AnalysisMessage `json:"messages"`
    Summary  string            `json:"summary"`
}
type AnalysisMessage struct {
    Code        string `json:"code"`        // e.g., IST0101
    Level       string `json:"level"`       // Error, Warning, Info
    Message     string `json:"message"`
    Resource    string `json:"resource"`
    Namespace   string `json:"namespace"`
    Suggestion  string `json:"suggestion,omitempty"`
}
```
- **When to use**: Validate configuration, detect misconfigurations
- **Wraps**: `istioctl/pkg/analyze/analyze.go` using the local analyzer framework
- **Covers**: 31+ analyzers detecting 40+ issue types (IST0101-IST0174)
- **Key detections**: Missing resources, conflicting policies, deprecated annotations, schema validation errors, injection issues

#### 3. Resource Description Tools

**DescribePodTool** — wraps `istioctl x describe pod`
```go
type DescribePodInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Name of the pod to describe"`
    Namespace string `json:"namespace" jsonschema:"required,description=Namespace of the pod"`
}
```
- **When to use**: Deep analysis of a specific pod's Istio configuration, routing, and policies
- **Provides**: Service association, VirtualService matching, DestinationRule analysis, PeerAuthentication, AuthorizationPolicy, ingress exposure

**DescribeServiceTool** — wraps `istioctl x describe svc`
```go
type DescribeServiceInput struct {
    ServiceName string `json:"service_name" jsonschema:"required,description=Name of the service to describe"`
    Namespace   string `json:"namespace" jsonschema:"required,description=Namespace of the service"`
}
```
- **When to use**: Analyze service-level routing, traffic policies, and endpoint health

#### 4. Authorization Policy Tool

**AuthzCheckTool** — wraps `istioctl x authz check`
```go
type AuthzCheckInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Pod to check authorization policies for"`
    Namespace string `json:"namespace" jsonschema:"required,description=Namespace of the pod"`
}
type AuthzCheckOutput struct {
    Policies []AuthzPolicy `json:"policies"`
    Summary  string        `json:"summary"`
}
type AuthzPolicy struct {
    Action string `json:"action"` // ALLOW, DENY, LOG, CUSTOM
    Name   string `json:"name"`   // policy.namespace format
    Rules  int    `json:"rules"`  // Number of rules
}
```
- **When to use**: Debug authorization policy issues, check what RBAC rules are applied to a pod
- **Data source**: Extracts policies from Envoy RBAC HTTP/TCP filters via config dump

#### 5. Pre-check Tool

**PrecheckTool** — wraps `istioctl x precheck`
```go
type PrecheckInput struct {
    FromVersion string `json:"from_version,omitempty" jsonschema:"description=Check upgrade compatibility from this version"`
}
```
- **When to use**: Before installation or upgrade, validate cluster readiness
- **Checks**: Kubernetes version, RBAC permissions, CRD compatibility, Gateway API version

#### 6. Kubernetes Context Tools

**KubernetesPodInfoTool**
```go
type PodInfoInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
```
- Returns: Pod status, containers, labels, annotations, events, restart counts
- **When to use**: Check basic pod health before diving into Istio-specific debugging

**KubernetesServiceInfoTool**
```go
type ServiceInfoInput struct {
    ServiceName string `json:"service_name" jsonschema:"required"`
    Namespace   string `json:"namespace" jsonschema:"required"`
}
```
- Returns: Service type, ports, selectors, endpoints, labels

**KubernetesNamespaceInfoTool**
```go
type NamespaceInfoInput struct {
    Namespace string `json:"namespace" jsonschema:"required"`
}
```
- Returns: Namespace labels (injection mode, ambient mode, waypoint assignment), annotations

**KubernetesPodLogsTool**
```go
type PodLogsInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Container string `json:"container,omitempty" jsonschema:"description=Container name (default: istio-proxy)"`
    TailLines int    `json:"tail_lines,omitempty" jsonschema:"description=Number of lines to return (default: 100)"`
    Since     string `json:"since,omitempty" jsonschema:"description=Duration to look back (e.g., 5m, 1h)"`
}
```
- Returns: Recent log lines from specified container
- **When to use**: Check for error patterns in sidecar/ztunnel/application logs

**KubernetesEventsTool**
```go
type EventsInput struct {
    Namespace string `json:"namespace" jsonschema:"required"`
    Resource  string `json:"resource,omitempty" jsonschema:"description=Filter events for specific resource (e.g., pod/my-pod)"`
}
```
- Returns: Recent Kubernetes events, useful for injection failures, scheduling issues

#### 7. Istiod Debug Tools

**IstiodDebugTool** — wraps `istioctl x internal-debug`
```go
type IstiodDebugInput struct {
    DebugType string `json:"debug_type" jsonschema:"required,description=Debug endpoint type (syncz, configz, registryz, endpointShardz, clusterz, authorizationz, etc.)"`
    ProxyID   string `json:"proxy_id,omitempty" jsonschema:"description=Optional proxy ID to filter results"`
}
```
- **When to use**: Deep control plane debugging when other tools don't provide enough info
- **Available debug types**: syncz, configz, registryz, endpointShardz, clusterz, networkz, authorizationz, telemetryz, push_status, mesh, inject

#### 8. Metrics Tool

**MetricsTool** — wraps `istioctl x metrics`
```go
type MetricsInput struct {
    WorkloadName string `json:"workload_name" jsonschema:"required,description=Workload to query metrics for"`
    Namespace    string `json:"namespace" jsonschema:"required"`
    Duration     string `json:"duration,omitempty" jsonschema:"description=Time window (default: 1m)"`
}
type MetricsOutput struct {
    TotalRPS   float64       `json:"total_rps"`
    ErrorRPS   float64       `json:"error_rps"`
    P50Latency time.Duration `json:"p50_latency"`
    P90Latency time.Duration `json:"p90_latency"`
    P99Latency time.Duration `json:"p99_latency"`
}
```
- **When to use**: Check workload health, error rates, latency
- **Requires**: Prometheus accessible in the cluster

## Implementation Approach

Each tool should:
1. Accept structured input (Go struct with JSON/jsonschema tags)
2. Call the appropriate istioctl package function programmatically (NOT via CLI subprocess)
3. Return structured output with a human-readable `summary` field
4. Handle errors gracefully with actionable error messages
5. Declare whether it's read-only or mutating
6. Declare which data plane modes it supports

### Code Reuse Strategy

Most tools should directly call existing functions from `istioctl/pkg/`:
- `proxystatus.XdsStatusCommand` logic → `ProxyStatusTool`
- `analyze.Analyze` logic → `AnalyzeTool`
- `proxyconfig.ProxyConfig` logic → `ProxyConfigTools`
- `describe.Cmd` logic → `DescribeTools`
- `authz.AuthZ` logic → `AuthzCheckTool`
- `ztunnelconfig.*` logic → `ZtunnelTools`

This requires refactoring some functions to return data structures instead of printing directly, or capturing their output.

## Implementation Steps

1. [ ] Define tool interface and base types in `tools/tool.go`
2. [ ] Implement `tools/registry.go` with MCP schema generation
3. [ ] Implement cluster state tools (proxy-status, version)
4. [ ] Implement config analysis tool (analyze)
5. [ ] Implement resource description tools (describe pod/svc)
6. [ ] Implement authorization policy tool (authz check)
7. [ ] Implement pre-check tool
8. [ ] Implement Kubernetes context tools (pod info, service info, logs, events, namespaces)
9. [ ] Implement Istiod debug tool
10. [ ] Implement metrics tool
11. [ ] Write unit tests for each tool with mock cluster data
12. [ ] Write integration tests with real istioctl package calls

## Acceptance Criteria

- [ ] All tools have well-defined input/output schemas
- [ ] All tools work programmatically (no subprocess execution)
- [ ] All tools return structured data with human-readable summaries
- [ ] Read-only tools don't require confirmation
- [ ] Tools gracefully handle missing resources, permissions errors, disconnected clusters
- [ ] Each tool has unit tests with mock data
