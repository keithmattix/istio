# Sub-issue 6: Sidecar-Only Features & Injection Tools

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement agent tools for features that are **truly sidecar-only** — specifically the Sidecar CRD (configuration scoping, exportTo), sidecar injection diagnostics, and outbound traffic interception patterns. These features have no ambient mode equivalent because waypoint proxies use a different architectural model.

> **Important note**: Envoy proxy-config tools (cluster, listener, route, endpoint, secret) are NOT sidecar-only. Waypoint proxies are also Envoy proxies and have listeners, clusters, routes, etc. Those tools are in [Sub-issue 07: Envoy Proxy Inspection Tools](./07-envoy-deep-inspection.md).

## Motivation

Certain Istio features exist only in sidecar mode because they depend on the per-pod proxy architecture:

1. **Sidecar CRD**: Controls per-workload egress/ingress listener configuration and visibility scoping. Waypoint proxies use a synthetic default scope that imports all exported services — they ignore the Sidecar CRD entirely.
2. **exportTo**: Service and VirtualService visibility scoping. In ambient mode, waypoints use `DefaultSidecarScopeForGateway()` which imports all exported services without per-workload filtering.
3. **Outbound traffic interception**: The virtual outbound listener (iptables-based `UseOriginalDst`) only exists on sidecar proxies. Waypoints only have inbound listeners.
4. **Sidecar injection**: The mutating webhook that adds istio-proxy containers. Ambient mode uses namespace labels and ztunnel instead.

## Detailed Design

### Sidecar CRD & Configuration Scoping Tools

#### SidecarScopeTool — inspects effective Sidecar CRD configuration
```go
type SidecarScopeInput struct {
    PodName   string `json:"pod_name" jsonschema:"required,description=Pod to check sidecar scope for"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type SidecarScopeOutput struct {
    HasSidecarCRD     bool              `json:"has_sidecar_crd"`
    SidecarName       string            `json:"sidecar_name,omitempty"`
    EgressListeners   []EgressListener  `json:"egress_listeners,omitempty"`
    IngressListeners  []IngressListener `json:"ingress_listeners,omitempty"`
    OutboundPolicy    string            `json:"outbound_policy"` // ALLOW_ANY or REGISTRY_ONLY
    ImportedServices  int               `json:"imported_services"`
    Summary           string            `json:"summary"`
}
type EgressListener struct {
    Port  int      `json:"port,omitempty"`
    Bind  string   `json:"bind,omitempty"`
    Hosts []string `json:"hosts"` // namespace/host format
}
type IngressListener struct {
    Port            int    `json:"port"`
    Protocol        string `json:"protocol"`
    DefaultEndpoint string `json:"default_endpoint"`
    TLS             bool   `json:"tls"`
}
```
- **When to use**: Debug service visibility issues where a sidecar can't see a destination service
- **Data source**: istiod debug endpoint `/debug/sidecarz` or XDS query
- **Mode**: Sidecar only — waypoints don't use Sidecar CRD
- **Key insight**: If no Sidecar CRD matches a workload, it uses a default scope that imports everything. Custom Sidecar CRDs restrict visibility.

#### ExportToAnalysisTool — checks service/VirtualService visibility
```go
type ExportToInput struct {
    ServiceName string `json:"service_name" jsonschema:"required,description=Service to check exportTo for"`
    Namespace   string `json:"namespace" jsonschema:"required"`
}
type ExportToOutput struct {
    ExportedToNamespaces []string `json:"exported_to_namespaces"`
    ExportedToAll        bool     `json:"exported_to_all"`
    VirtualServices      []VSExportInfo `json:"virtual_services,omitempty"`
    Summary              string   `json:"summary"`
}
type VSExportInfo struct {
    Name      string   `json:"name"`
    Namespace string   `json:"namespace"`
    ExportTo  []string `json:"export_to"`
}
```
- **When to use**: Debug "service not found" issues where exportTo restricts visibility
- **Mode**: Primarily sidecar — waypoints import all exported services, but exportTo still controls _what_ is exported
- **Key insight**: `exportTo: ["."]` restricts a service to its own namespace; `exportTo: ["*"]` makes it globally visible

#### OutboundPolicyTool — checks outbound traffic policy
```go
type OutboundPolicyInput struct {
    PodName   string `json:"pod_name" jsonschema:"required"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type OutboundPolicyOutput struct {
    MeshPolicy    string `json:"mesh_policy"`     // ALLOW_ANY or REGISTRY_ONLY
    SidecarPolicy string `json:"sidecar_policy"`  // Override from Sidecar CRD
    EffectivePolicy string `json:"effective_policy"`
    PassthroughClusterExists bool `json:"passthrough_cluster_exists"`
    Summary       string `json:"summary"`
}
```
- **When to use**: Debug "connection refused to external service" issues
- **Mode**: Sidecar only — waypoints don't have outbound listeners or PassthroughCluster
- **Key insight**: `REGISTRY_ONLY` blocks traffic to services not in the mesh registry; `ALLOW_ANY` forwards unknown traffic via PassthroughCluster

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
- **Mode**: Sidecar only

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
- **Mode**: Sidecar only

### Sidecar-Specific Troubleshooting Workflows

#### Workflow: "Sidecar not injected"
1. `InjectorStatusTool` — Check webhooks are configured
2. `KubernetesNamespaceInfoTool` — Check namespace labels (`istio-injection=enabled`)
3. `KubernetesEventsTool` — Check for webhook failure events
4. `InjectionCheckTool` — Check specific pod injection status
5. Guide: Check for `sidecar.istio.io/inject: "false"` annotation on pod

#### Workflow: "Service not visible to sidecar"
1. `SidecarScopeTool` — Check if a custom Sidecar CRD restricts visibility
2. `ExportToAnalysisTool` — Check if the target service's exportTo restricts visibility
3. `ProxyConfigClusterTool` — Verify the destination cluster exists in Envoy
4. Guide: If using Sidecar CRD, ensure `hosts` includes the target namespace/service

#### Workflow: "Can't reach external service"
1. `OutboundPolicyTool` — Check if REGISTRY_ONLY blocks external traffic
2. `ProxyConfigClusterTool` — Check for PassthroughCluster
3. `AnalyzeTool` — Check for ServiceEntry misconfigurations
4. Guide: Use ServiceEntry to register external services, or switch to ALLOW_ANY

#### Workflow: "Traffic routing not working (sidecar)"
1. `AnalyzeTool` — Check for VirtualService/DestinationRule misconfigurations
2. `SidecarScopeTool` — Check if Sidecar CRD egress listeners import the relevant VS
3. `ProxyConfigRouteTool` on source pod — Check if route exists
4. `ProxyConfigClusterTool` on source pod — Check destination cluster and subset
5. `ProxyConfigDiffTool` — Check for config sync issues

### What is NOT in this sub-issue

The following tools apply to **all Envoy proxies** (sidecar, waypoint, and gateway) and are covered in [Sub-issue 07](./07-envoy-deep-inspection.md):
- `proxy-config cluster` — Envoy cluster inspection
- `proxy-config listener` — Envoy listener inspection
- `proxy-config route` — Envoy route inspection
- `proxy-config endpoint` — Envoy endpoint inspection
- `proxy-config secret` — Envoy secret/certificate inspection
- `proxy-config bootstrap` — Envoy bootstrap configuration
- `proxy-config log` — Envoy log level management
- `proxy-status` (XDS sync) — Works for any proxy connected to istiod
- `authz check` — RBAC policy extraction from any Envoy config dump

## Implementation Steps

1. [ ] Implement `SidecarScopeTool`
2. [ ] Implement `ExportToAnalysisTool`
3. [ ] Implement `OutboundPolicyTool`
4. [ ] Implement `InjectionCheckTool`
5. [ ] Implement `InjectorStatusTool`
6. [ ] Encode sidecar-specific troubleshooting workflows in system prompt
7. [ ] Write unit tests with mock Sidecar CRD and injection data
8. [ ] Write integration tests

## Acceptance Criteria

- [ ] SidecarScope tool correctly reads Sidecar CRD and shows effective scope
- [ ] ExportTo tool identifies visibility restrictions on services and VirtualServices
- [ ] OutboundPolicy tool detects REGISTRY_ONLY vs ALLOW_ANY and PassthroughCluster presence
- [ ] Injection check correctly identifies injected vs non-injected pods
- [ ] Agent follows sidecar-specific troubleshooting workflows
- [ ] Tools correctly identify when they're not applicable (e.g., running against a waypoint pod)
- [ ] Output includes actionable suggestions for common sidecar scoping issues
