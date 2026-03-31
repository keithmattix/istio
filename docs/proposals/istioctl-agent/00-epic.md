# [Epic] istioctl AI Troubleshooting Agent

**Labels**: `enhancement`, `area/istioctl`

## Summary

Add an AI-powered troubleshooting agent to `istioctl` that helps users diagnose and resolve Istio service mesh issues through natural language interaction. The agent leverages existing istioctl capabilities (analyze, proxy-config, proxy-status, describe, ztunnel-config, waypoint, etc.) but exposes them through a conversational interface that guides users through troubleshooting workflows.

## Motivation

Istio is a powerful but complex service mesh. Users frequently struggle with:
- **Configuration issues**: Misconfigured VirtualServices, DestinationRules, AuthorizationPolicies
- **mTLS failures**: Certificate errors, PeerAuthentication mismatches, SDS issues
- **Sidecar injection problems**: Missing sidecars, webhook failures, namespace labeling
- **Ambient mode complexity**: Waypoint deployment, ztunnel debugging, mode transitions
- **Boundary confusion**: Not knowing whether an issue is Istio, CNI, CoreDNS, or Kubernetes NetworkPolicy

The agent should understand these boundaries and guide users to the right diagnostic path, including directing them to check non-Istio components (CNI, CoreDNS, kube-proxy) when appropriate.

## Architecture Overview

### Agent Framework
- **Primary framework**: Official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`) for tool registration and protocol compliance
- **Supplementary**: LangChainGo (`github.com/tmc/langchaingo`) for the ReAct agent loop, LLM provider abstraction, and function calling
- **No MCP server in istiod** — all operations run client-side in istioctl
- Agent registered as `istioctl x agent` (experimental command)

### Tool Architecture
The agent exposes istioctl's existing capabilities as composable tools:

| Tool Category | Wraps | Purpose |
|---|---|---|
| **Cluster State** | `proxy-status`, `version` | XDS sync, control plane health |
| **Proxy Config** | `proxy-config cluster/listener/route/endpoint/secret/log` | Envoy sidecar inspection |
| **Config Analysis** | `analyze` | 31+ analyzers, 40+ issue types |
| **Resource Description** | `describe pod/svc` | Traffic flow tracing, policy analysis |
| **Auth Policy** | `authz check` | RBAC policy extraction from Envoy |
| **Ztunnel** | `ztunnel-config workload/services/policy/connections` | Ambient mode debugging |
| **Waypoint** | `waypoint status/list` | Waypoint health and configuration |
| **Pre-check** | `precheck` | Installation/upgrade readiness |
| **Kubernetes** | kubectl wrappers | Pod logs, events, namespace labels, service details |
| **Envoy Admin** | Direct admin API | Stats, logging levels, config dump |
| **Istiod Debug** | `internal-debug` | syncz, configz, registryz, endpointShardz |

### Guided Operations Framework
Beyond troubleshooting, the agent supports guided operations that help users move from basic to advanced Istio usage:
- **Waypoint Deployment**: Examine cluster, ask clarifying questions (granularity, autoscaling), generate and optionally apply configuration
- **mTLS Migration**: Guide users through PERMISSIVE → STRICT migration
- **Traffic Management**: Help configure canary deployments, traffic shifting
- **Ambient Migration**: Guide sidecar → ambient mode transition
- **Multi-cluster Setup**: Assist with remote cluster configuration

**All cluster-modifying operations MUST prompt the user for confirmation.**

### Data Plane Mode Awareness
The agent MUST understand and handle both data plane modes:

**Sidecar Mode**:
- Traffic intercepted by iptables → Envoy sidecar
- Debug via `proxy-config`, `proxy-status`
- Injection controlled by namespace label `istio-injection=enabled`

**Ambient Mode**:
- Traffic intercepted by ztunnel (node-level) + optional waypoint (L7)
- Debug via `ztunnel-config`, `waypoint status`
- Enrollment via namespace label `istio.io/dataplane-mode=ambient`
- Waypoints use Gateway API (`GatewayClassName: istio-waypoint-class`)

### Boundary Awareness
The agent should detect and communicate when issues may lie outside Istio:

| Component | Istio Boundary | Redirect Guidance |
|---|---|---|
| **CNI Plugin** | Istio CNI chains after primary CNI | Check primary CNI (Calico/Cilium/Flannel) for IP allocation, routing |
| **CoreDNS** | Istio DNS proxy intercepts, falls back to CoreDNS | Check CoreDNS for resolution failures, search domain issues |
| **NetworkPolicies** | Istio uses AuthorizationPolicy (L7), NOT K8s NetworkPolicy (L3/L4) | Both can block traffic independently |
| **kube-proxy** | Istio replaces kube-proxy service routing | Check kube-proxy for non-mesh service issues |

## Sub-Issues

1. **[Sub-issue 01](./01-core-agent-framework.md)**: Core Agent Framework & CLI Integration
2. **[Sub-issue 02](./02-diagnostic-tools.md)**: Diagnostic Tool Wrappers
3. **[Sub-issue 03](./03-guided-operations.md)**: Guided Operations Framework
4. **[Sub-issue 04](./04-boundary-detection.md)**: Boundary Detection & Cross-Component Awareness
5. **[Sub-issue 05](./05-ambient-mode-tools.md)**: Ambient Mode Specialized Tools
6. **[Sub-issue 06](./06-sidecar-mode-tools.md)**: Sidecar Mode Specialized Tools
7. **[Sub-issue 07](./07-envoy-deep-inspection.md)**: Envoy Deep Inspection Tools
8. **[Sub-issue 08](./08-testing-docs.md)**: Testing, Documentation & UX

## Design Principles

1. **Non-intrusive**: No changes to istiod or the control plane; everything runs in istioctl
2. **Extensible**: Tool and guided-operation registration patterns that make it easy to add new capabilities
3. **Safe**: All cluster-modifying operations require explicit user confirmation
4. **Mode-aware**: Always detect and adapt to sidecar vs ambient mode
5. **Boundary-aware**: Know when to direct users to non-Istio components
6. **Offline-capable**: Core diagnostic logic works without LLM; LLM enhances natural language interaction
