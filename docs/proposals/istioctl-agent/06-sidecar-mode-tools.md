# Sub-issue 6: Sidecar Mode Specialized Tools

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement specialized agent tools for debugging sidecar-mode deployments, including proxy configuration inspection, XDS sync verification, sidecar injection diagnostics, and sidecar-specific troubleshooting workflows.

## Motivation

Sidecar mode is the traditional and still widely-used Istio data plane. Debugging sidecar issues requires inspecting per-pod Envoy configuration, verifying XDS synchronization with istiod, checking injection status, and understanding iptables traffic redirection. These tools wrap existing `proxy-config` and `proxy-status` commands with structured output.

## Detailed Design

### Proxy Configuration Tools

#### ProxyConfigClusterTool — wraps `istioctl proxy-config cluster`
```go
type ProxyConfigClusterInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    FQDN      string `json:"fqdn,omitempty" jsonschema:"description=Filter by service FQDN"`
    Port      int    `json:"port,omitempty" jsonschema:"description=Filter by port"`
    Direction string `json:"direction,omitempty" jsonschema:"enum=inbound,outbound"`
}
type ProxyConfigClusterOutput struct {
    Clusters []EnvoyCluster `json:"clusters"`
    Summary  string         `json:"summary"`
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
- **When to use**: Check if Envoy knows about a destination service, verify DestinationRule application
- **Data source**: Envoy admin API `config_dump?mask=dynamic_active_clusters,...`

#### ProxyConfigListenerTool — wraps `istioctl proxy-config listener`
```go
type ProxyConfigListenerInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Port      int    `json:"port,omitempty"`
    Type      string `json:"type,omitempty" jsonschema:"enum=HTTP,TCP,HTTP+TCP"`
    Address   string `json:"address,omitempty"`
}
type ProxyConfigListenerOutput struct {
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
- **When to use**: Check if traffic is being intercepted on the right ports
- **Key insight**: Inbound listeners (0.0.0.0:15006) and outbound listener (0.0.0.0:15001) are critical

#### ProxyConfigRouteTool — wraps `istioctl proxy-config route`
```go
type ProxyConfigRouteInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    RouteName string `json:"route_name,omitempty"`
}
type ProxyConfigRouteOutput struct {
    Routes  []EnvoyRoute `json:"routes"`
    Summary string       `json:"summary"`
}
type EnvoyRoute struct {
    Name           string `json:"name"`
    VirtualHost    string `json:"virtual_host"`
    Domains        string `json:"domains"`
    Match          string `json:"match"`
    VirtualService string `json:"virtual_service,omitempty"`
}
```
- **When to use**: Debug routing decisions, verify VirtualService application

#### ProxyConfigEndpointTool — wraps `istioctl proxy-config endpoint`
```go
type ProxyConfigEndpointInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
    Cluster   string `json:"cluster,omitempty" jsonschema:"description=Filter by cluster name"`
    Status    string `json:"status,omitempty" jsonschema:"enum=HEALTHY,UNHEALTHY,UNKNOWN"`
}
type ProxyConfigEndpointOutput struct {
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
- **When to use**: Check endpoint health, verify service discovery

#### ProxyConfigSecretTool — wraps `istioctl proxy-config secret`
```go
type ProxyConfigSecretInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type ProxyConfigSecretOutput struct {
    Secrets []EnvoySecret `json:"secrets"`
    Summary string        `json:"summary"`
}
type EnvoySecret struct {
    Name       string    `json:"name"`
    Type       string    `json:"type"` // Cert, Root CA, Validation Context
    ValidFrom  time.Time `json:"valid_from"`
    ValidUntil time.Time `json:"valid_until"`
    SerialNum  string    `json:"serial_number"`
}
```
- **When to use**: Debug mTLS certificate issues, check cert expiration

### Sidecar Injection Tools

#### InjectionCheckTool — wraps `istioctl x check-inject`
```go
type InjectionCheckInput struct {
    PodName   string `json:"pod_name,omitempty"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type InjectionCheckOutput struct {
    Pods    []InjectionStatus `json:"pods"`
    Summary string            `json:"summary"`
}
type InjectionStatus struct {
    PodName      string `json:"pod_name"`
    Injected     bool   `json:"injected"`
    Reason       string `json:"reason,omitempty"` // Why not injected
    ProxyVersion string `json:"proxy_version,omitempty"`
    ProxyStatus  string `json:"proxy_status,omitempty"` // Running, CrashLoopBackOff, etc.
}
```
- **When to use**: Verify sidecar injection, find pods missing sidecars

#### InjectorStatusTool
```go
type InjectorStatusInput struct{}
type InjectorStatusOutput struct {
    Webhooks []WebhookStatus `json:"webhooks"`
    Summary  string          `json:"summary"`
}
type WebhookStatus struct {
    Name      string `json:"name"`
    Revision  string `json:"revision"`
    Namespace string `json:"namespace"`
    Tag       string `json:"tag,omitempty"`
}
```
- **When to use**: Check if sidecar injection webhooks are properly configured

### XDS Sync Comparison Tool

**ProxyConfigDiffTool** — wraps `istioctl proxy-status <pod>` (detailed comparison mode)
```go
type ProxyConfigDiffInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type ProxyConfigDiffOutput struct {
    ClusterDiff  string `json:"cluster_diff,omitempty"`
    ListenerDiff string `json:"listener_diff,omitempty"`
    RouteDiff    string `json:"route_diff,omitempty"`
    InSync       bool   `json:"in_sync"`
    Summary      string `json:"summary"`
}
```
- **When to use**: Detect config drift between what istiod sent and what Envoy has
- **Wraps**: `istioctl/pkg/writer/compare/` comparator

### Sidecar Troubleshooting Workflows

#### Workflow: "503 errors from my service"
1. `ProxyStatusTool` — Check if sidecar is in sync with istiod
2. `ProxyConfigClusterTool` — Check destination cluster exists
3. `ProxyConfigEndpointTool` — Check endpoints are HEALTHY
4. `ProxyConfigRouteTool` — Check route matches correctly
5. `ProxyConfigSecretTool` — Check if mTLS certs are valid
6. `KubernetesPodLogsTool(container=istio-proxy)` — Check sidecar logs for errors
7. If mTLS issue: Check PeerAuthentication and DestinationRule TLS settings

#### Workflow: "Sidecar not injected"
1. `InjectorStatusTool` — Check webhooks are configured
2. `KubernetesNamespaceInfoTool` — Check namespace labels (`istio-injection=enabled`)
3. `KubernetesEventsTool` — Check for webhook failure events
4. `InjectionCheckTool` — Check specific pod injection status
5. Guide: Check for `sidecar.istio.io/inject: "false"` annotation on pod

#### Workflow: "Traffic routing not working"
1. `AnalyzeTool` — Check for VirtualService/DestinationRule misconfigurations
2. `ProxyConfigRouteTool` on source pod — Check if route exists
3. `ProxyConfigClusterTool` on source pod — Check destination cluster and subset
4. `DescribePodTool` — Trace full traffic path
5. `ProxyConfigDiffTool` — Check for config sync issues

#### Workflow: "mTLS handshake failure"
1. `ProxyConfigSecretTool` on both source and destination — Check certs
2. `DescribePodTool` — Check PeerAuthentication mode
3. `AuthzCheckTool` — Check AuthorizationPolicies
4. `BoundaryCheck(CertManager)` — Check if using external CA
5. Guide: STRICT mode requires both sides to have valid mTLS certs

## Implementation Steps

1. [ ] Implement `ProxyConfigClusterTool`
2. [ ] Implement `ProxyConfigListenerTool`
3. [ ] Implement `ProxyConfigRouteTool`
4. [ ] Implement `ProxyConfigEndpointTool`
5. [ ] Implement `ProxyConfigSecretTool`
6. [ ] Implement `InjectionCheckTool`
7. [ ] Implement `InjectorStatusTool`
8. [ ] Implement `ProxyConfigDiffTool`
9. [ ] Encode sidecar troubleshooting workflows in system prompt
10. [ ] Write unit tests with mock Envoy config dumps
11. [ ] Write integration tests

## Acceptance Criteria

- [ ] All proxy-config tools return structured data from Envoy config dump
- [ ] Tools correctly parse config dump protobuf structures
- [ ] Injection check correctly identifies injected vs non-injected pods
- [ ] Config diff tool produces meaningful comparison results
- [ ] Agent follows sidecar-specific troubleshooting workflows
- [ ] Output includes actionable suggestions for common issues
