# Sub-issue 5: Ambient Mode Specialized Tools

**Labels**: `enhancement`, `area/istioctl`, `area/ambient`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement specialized agent tools for debugging and managing ambient mode deployments, including ztunnel inspection, waypoint management, HBONE tunneling diagnostics, and ambient-specific troubleshooting workflows.

## Motivation

Ambient mode introduces fundamentally different debugging patterns compared to sidecar mode. Instead of per-pod sidecars, traffic flows through node-level ztunnel proxies and optional per-service waypoint proxies. Users need tools that understand this architecture and can trace traffic through ztunnel → waypoint paths.

## Detailed Design

### Mode Detection Tool

**ClusterModeTool** — automatically detects data plane mode
```go
type ClusterModeInput struct {
    Namespace string `json:"namespace,omitempty" jsonschema:"description=Specific namespace to check (empty for all)"`
}
type ClusterModeOutput struct {
    Namespaces []NamespaceMode `json:"namespaces"`
    Summary    string          `json:"summary"`
}
type NamespaceMode struct {
    Name           string `json:"name"`
    Mode           string `json:"mode"` // "ambient", "sidecar", "none"
    WaypointName   string `json:"waypoint_name,omitempty"`
    WaypointStatus string `json:"waypoint_status,omitempty"` // "Ready", "NotReady", "None"
    PodCount       int    `json:"pod_count"`
    MeshedPodCount int    `json:"meshed_pod_count"`
}
```
- **When to use**: First step in any troubleshooting session to understand the cluster's data plane mode
- **Implementation**: Check namespace labels (`istio.io/dataplane-mode`, `istio-injection`), count pods with sidecars vs ambient enrollment

### Ztunnel Tools

#### ZtunnelWorkloadTool — wraps `istioctl ztunnel-config workload`
```go
type ZtunnelWorkloadInput struct {
    Namespace    string `json:"namespace,omitempty"`
    WorkloadName string `json:"workload_name,omitempty"`
    Node         string `json:"node,omitempty" jsonschema:"description=Filter by node name"`
}
type ZtunnelWorkloadOutput struct {
    Workloads []ZtunnelWorkload `json:"workloads"`
    Summary   string            `json:"summary"`
}
type ZtunnelWorkload struct {
    Name             string `json:"name"`
    Namespace        string `json:"namespace"`
    Address          string `json:"address"`
    Node             string `json:"node"`
    WaypointAddress  string `json:"waypoint_address,omitempty"`
    Protocol         string `json:"protocol"`
    ServiceAccount   string `json:"service_account"`
}
```
- **When to use**: Check if ztunnel has discovered workloads, verify waypoint assignments
- **Wraps**: `istioctl/pkg/ztunnelconfig/ztunnelconfig.go`

#### ZtunnelServicesTool — wraps `istioctl ztunnel-config services`
```go
type ZtunnelServicesInput struct {
    Namespace   string `json:"namespace,omitempty"`
    ServiceName string `json:"service_name,omitempty"`
}
type ZtunnelServicesOutput struct {
    Services []ZtunnelService `json:"services"`
    Summary  string           `json:"summary"`
}
type ZtunnelService struct {
    Name       string   `json:"name"`
    Namespace  string   `json:"namespace"`
    VIPs       []string `json:"vips"`
    Endpoints  int      `json:"endpoint_count"`
    Waypoint   string   `json:"waypoint,omitempty"`
}
```
- **When to use**: Verify service discovery in ztunnel, check VIP assignments and waypoint bindings

#### ZtunnelPolicyTool — wraps `istioctl ztunnel-config policy`
```go
type ZtunnelPolicyInput struct {
    Namespace string `json:"namespace,omitempty"`
}
type ZtunnelPolicyOutput struct {
    Policies []ZtunnelPolicy `json:"policies"`
    Summary  string          `json:"summary"`
}
type ZtunnelPolicy struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
    Action    string `json:"action"` // ALLOW, DENY
    Rules     int    `json:"rules"`
    DryRun    bool   `json:"dry_run"`
}
```
- **When to use**: Check what authorization policies ztunnel enforces at L4
- **Key insight**: In ambient mode, L4 policies are enforced by ztunnel; L7 policies are enforced by waypoint

#### ZtunnelConnectionsTool — wraps `istioctl ztunnel-config connections`
```go
type ZtunnelConnectionsInput struct {
    Namespace string `json:"namespace,omitempty"`
    Direction string `json:"direction,omitempty" jsonschema:"enum=inbound,outbound"`
}
type ZtunnelConnectionsOutput struct {
    Connections []ZtunnelConnection `json:"connections"`
    Summary     string              `json:"summary"`
}
type ZtunnelConnection struct {
    Source      string `json:"source"`
    Destination string `json:"destination"`
    State       string `json:"state"`
    Protocol    string `json:"protocol"` // HBONE, TCP
}
```
- **When to use**: Check active connections through ztunnel, verify traffic is flowing

#### ZtunnelCertificateTool — wraps `istioctl ztunnel-config certificate`
```go
type ZtunnelCertInput struct {
    Namespace string `json:"namespace,omitempty"`
}
type ZtunnelCertOutput struct {
    Certificates []ZtunnelCert `json:"certificates"`
    Summary      string        `json:"summary"`
}
type ZtunnelCert struct {
    Identity   string    `json:"identity"` // SPIFFE ID
    State      string    `json:"state"`    // Active, Warming
    ValidFrom  time.Time `json:"valid_from"`
    ValidUntil time.Time `json:"valid_until"`
}
```
- **When to use**: Verify mTLS certificate status in ambient mode

### Waypoint Tools

#### WaypointStatusTool — wraps `istioctl waypoint status`
```go
type WaypointStatusInput struct {
    Name      string `json:"name,omitempty"`
    Namespace string `json:"namespace" jsonschema:"required"`
}
type WaypointStatusOutput struct {
    Waypoints []WaypointInfo `json:"waypoints"`
    Summary   string         `json:"summary"`
}
type WaypointInfo struct {
    Name        string `json:"name"`
    Namespace   string `json:"namespace"`
    TrafficType string `json:"traffic_type"` // service, workload, all
    Status      string `json:"status"`       // Programmed, NotProgrammed
    Addresses   []string `json:"addresses"`
    GatewayClass string `json:"gateway_class"`
}
```
- **When to use**: Check waypoint health and configuration

#### WaypointListTool — wraps `istioctl waypoint list`
```go
type WaypointListInput struct {
    Namespace     string `json:"namespace,omitempty"`
    AllNamespaces bool   `json:"all_namespaces,omitempty"`
}
```
- **When to use**: Discover all waypoints in the cluster

### Ambient Troubleshooting Workflows

The agent should use these tools in specific patterns for common ambient issues:

#### Workflow: "Traffic not reaching my service in ambient mode"
1. `ClusterModeTool` — Verify namespace is in ambient mode
2. `ZtunnelWorkloadTool` — Check ztunnel sees the workload
3. `ZtunnelServicesTool` — Check service is registered with correct VIP
4. `ZtunnelConnectionsTool` — Check if connections are being established
5. `WaypointStatusTool` — If L7 features needed, check waypoint is ready
6. `ZtunnelPolicyTool` — Check authorization policies aren't blocking
7. `BoundaryCheck(CNI)` — Check CNI enrollment if ztunnel doesn't see the workload

#### Workflow: "Authorization policy not working in ambient"
1. `ClusterModeTool` — Verify ambient mode
2. `ZtunnelPolicyTool` — Check L4 policies in ztunnel
3. `WaypointStatusTool` — L7 policies require a waypoint
4. `AuthzCheckTool` on waypoint pod — Check L7 policies in waypoint Envoy
5. Guide user: "L4 policies (principal, namespace) are enforced by ztunnel. L7 policies (paths, headers, methods) require a waypoint."

#### Workflow: "Waypoint not processing traffic"
1. `WaypointStatusTool` — Is waypoint Programmed?
2. `ZtunnelServicesTool` — Does ztunnel know about the waypoint?
3. `ZtunnelWorkloadTool` — Are workloads/services using this waypoint?
4. Check namespace labels — Is `istio.io/use-waypoint` set?
5. Check Gateway API — Is GatewayClass `istio-waypoint-class` available?

## Implementation Steps

1. [ ] Implement `ClusterModeTool` for mode detection
2. [ ] Implement `ZtunnelWorkloadTool`
3. [ ] Implement `ZtunnelServicesTool`
4. [ ] Implement `ZtunnelPolicyTool`
5. [ ] Implement `ZtunnelConnectionsTool`
6. [ ] Implement `ZtunnelCertificateTool`
7. [ ] Implement `WaypointStatusTool`
8. [ ] Implement `WaypointListTool`
9. [ ] Encode ambient troubleshooting workflows in system prompt
10. [ ] Write unit tests with mock ztunnel data
11. [ ] Write integration tests

## Acceptance Criteria

- [ ] All ztunnel tools return structured data from ztunnel config dump
- [ ] Mode detection correctly identifies ambient vs sidecar vs none per namespace
- [ ] Waypoint tools integrate with Gateway API resources
- [ ] Agent follows ambient-specific troubleshooting workflows
- [ ] Clear distinction between L4 (ztunnel) and L7 (waypoint) policy enforcement
- [ ] Works with multi-node clusters (ztunnel is per-node)
