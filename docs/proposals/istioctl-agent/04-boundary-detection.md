# Sub-issue 4: Boundary Detection & Cross-Component Awareness

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement boundary detection logic that helps the agent distinguish between Istio-specific issues and problems that originate in other Kubernetes networking components (CNI, CoreDNS, kube-proxy, NetworkPolicies). The agent should provide clear guidance when issues cross these boundaries.

## Motivation

One of the most confusing aspects of troubleshooting Istio is understanding where Istio's responsibility ends and other components begin. Users often waste hours debugging Istio configuration when the root cause is a misconfigured NetworkPolicy, a broken CNI, or a DNS resolution issue in CoreDNS. The agent should be smart about detecting these boundary conditions.

## Detailed Design

### Boundary Detection Framework

```go
// BoundaryCheck represents a check that determines if an issue crosses the Istio boundary
type BoundaryCheck struct {
    Name        string
    Component   Component     // CNI, CoreDNS, NetworkPolicy, KubeProxy, CertManager
    Description string
    Check       func(ctx context.Context, cliCtx cli.Context, symptoms Symptoms) (*BoundaryResult, error)
}

type Component string
const (
    ComponentCNI           Component = "cni"
    ComponentCoreDNS       Component = "coredns"
    ComponentNetworkPolicy Component = "network-policy"
    ComponentKubeProxy     Component = "kube-proxy"
    ComponentCertManager   Component = "cert-manager"
    ComponentGatewayAPI    Component = "gateway-api"
)

type BoundaryResult struct {
    LikelyIstio    bool     // Is this likely an Istio issue?
    LikelyExternal bool     // Is this likely an external component issue?
    Component      Component
    Confidence     float64  // 0.0-1.0
    Evidence       []string // What evidence supports this conclusion
    Guidance       []string // What the user should check/do
}

type Symptoms struct {
    ConnectionRefused bool
    DNSFailure        bool
    TLSHandshakeFail  bool
    HTTP503           bool
    HTTP502           bool
    HTTP404           bool
    TimeoutError      bool
    NoRouteFound      bool
    PodNotReady       bool
    // ... extensible
}
```

### Boundary Checks

#### 1. CNI Boundary Detection

**When to check**: Pod networking failures, connection refused errors, IP allocation issues, ambient mode enrollment failures

**Checks**:
- Is the Istio CNI DaemonSet running on all nodes? (`istio-cni-node` pods)
- Are there CNI plugin errors in pod events? (look for `FailedCreatePodSandBox`)
- Is the primary CNI healthy? (check CNI config files in `/etc/cni/net.d/`)
- For ambient: Is the CNI agent socket responsive? (`istio-cni-socket`)
- Pod IP allocation working? (pods in `ContainerCreating` state)
- CNI repair controller active? (check for repair pods)

**Implementation**:
```go
func CheckCNIBoundary(ctx context.Context, cliCtx cli.Context, symptoms Symptoms) (*BoundaryResult, error) {
    kubeClient, _ := cliCtx.CLIClient()

    // Check istio-cni-node DaemonSet
    cniPods, _ := kubeClient.Kube().CoreV1().Pods(cliCtx.IstioNamespace()).
        List(ctx, metav1.ListOptions{LabelSelector: "k8s-app=istio-cni-node"})

    // Check for FailedCreatePodSandBox events
    events, _ := kubeClient.Kube().CoreV1().Events("").
        List(ctx, metav1.ListOptions{FieldSelector: "reason=FailedCreatePodSandBox"})

    // Analyze and return
    result := &BoundaryResult{Component: ComponentCNI}
    if len(events.Items) > 0 {
        result.LikelyExternal = true
        result.Confidence = 0.8
        result.Evidence = append(result.Evidence, fmt.Sprintf("Found %d FailedCreatePodSandBox events", len(events.Items)))
        result.Guidance = append(result.Guidance, "Check the primary CNI plugin logs (e.g., calico-node, cilium-agent)")
        result.Guidance = append(result.Guidance, "Verify CNI configuration in /etc/cni/net.d/ on affected nodes")
    }
    return result, nil
}
```

**Guidance messages**:
- "This appears to be a CNI networking issue, not Istio. Check your primary CNI plugin (Calico/Cilium/Flannel) logs."
- "The Istio CNI plugin chains AFTER the primary CNI. If pods can't get IPs, the issue is in the primary CNI."
- "For ambient mode: Check `istio-cni-node` pod logs on the affected node for enrollment errors."

#### 2. DNS Boundary Detection

**When to check**: DNS resolution failures, service discovery issues, `NXDOMAIN` errors

**Checks**:
- Is CoreDNS running and healthy?
- Is the Istio DNS proxy enabled? (`ISTIO_META_DNS_CAPTURE` env var in sidecar)
- Can the sidecar resolve the destination service?
- Is it a search domain issue (short name vs FQDN)?
- Check `/etc/resolv.conf` in the pod for correct nameserver

**Guidance messages**:
- "DNS resolution failure. Istio's DNS proxy (if enabled) intercepts queries but falls back to CoreDNS."
- "Check if CoreDNS pods are running: `kubectl get pods -n kube-system -l k8s-app=kube-dns`"
- "Try resolving the service FQDN directly: `kubectl exec <pod> -- nslookup <svc>.<ns>.svc.cluster.local`"
- "If using headless services, DNS returns pod IPs directly; check that pods are Ready."

#### 3. NetworkPolicy Boundary Detection

**When to check**: Connection refused/timeout when Istio configuration appears correct

**Checks**:
- Are there Kubernetes NetworkPolicies in the affected namespaces?
- Do the NetworkPolicies allow Istio control plane communication (port 15012, 15017)?
- Do they allow sidecar-to-sidecar communication?
- Do they allow ztunnel communication (ambient mode)?

**Implementation**:
```go
func CheckNetworkPolicyBoundary(ctx context.Context, cliCtx cli.Context, namespace string) (*BoundaryResult, error) {
    kubeClient, _ := cliCtx.CLIClient()

    // List NetworkPolicies
    policies, _ := kubeClient.Kube().NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})

    result := &BoundaryResult{Component: ComponentNetworkPolicy}
    if len(policies.Items) > 0 {
        for _, policy := range policies.Items {
            // Check if policy blocks Istio ports
            if blocksIstioControlPlane(policy) {
                result.LikelyExternal = true
                result.Confidence = 0.9
                result.Evidence = append(result.Evidence, fmt.Sprintf("NetworkPolicy %q may block Istio control plane ports", policy.Name))
            }
            if blocksIstiodataPlane(policy) {
                result.Evidence = append(result.Evidence, fmt.Sprintf("NetworkPolicy %q may block sidecar/ztunnel traffic", policy.Name))
            }
        }
        result.Guidance = append(result.Guidance,
            "Kubernetes NetworkPolicies operate at L3/L4, independent of Istio's L7 AuthorizationPolicies.",
            "Both can block traffic. Check that NetworkPolicies allow Istio control plane (15012, 15017) and data plane traffic.",
            "For ambient mode, ensure NetworkPolicies allow ztunnel node-level communication.",
        )
    }
    return result, nil
}
```

**Guidance messages**:
- "Found Kubernetes NetworkPolicies in namespace `X`. These operate independently from Istio AuthorizationPolicies."
- "Istio AuthorizationPolicies work at L7 (HTTP/gRPC). NetworkPolicies work at L3/L4 (IP/port). Both must ALLOW traffic for it to flow."
- "Ensure NetworkPolicies allow: port 15012 (XDS), 15017 (webhooks), 15008 (HBONE/ambient), 15001/15006 (sidecar)."

#### 4. kube-proxy / Service Mesh Boundary Detection

**When to check**: Non-mesh services can't reach mesh services, or vice versa

**Checks**:
- Is the pod in the mesh? (has sidecar or is in ambient namespace)
- Is the destination in the mesh?
- Are there `externalTrafficPolicy` issues?
- NodePort/LoadBalancer service interaction with mesh

**Guidance messages**:
- "Traffic from non-mesh pods to mesh services bypasses Istio entirely and uses kube-proxy routing."
- "If you need mTLS between mesh and non-mesh workloads, use PeerAuthentication with mode PERMISSIVE."

#### 5. Certificate Manager Boundary Detection

**When to check**: TLS certificate errors, mTLS failures

**Checks**:
- Is cert-manager installed? (check CRDs)
- Are Istio's CA certificates valid? (`istioctl proxy-config secret`)
- Is the root CA expired or rotating?
- Is there a custom CA integration (cert-manager istio-csr)?

**Guidance messages**:
- "Certificate error detected. If using cert-manager with istio-csr, check cert-manager logs."
- "Istio's built-in CA (istiod) manages mesh certificates by default. Check with `istioctl proxy-config secret <pod>`."

### Integration with Agent

The boundary detection runs as a **pre-analysis step** when the agent detects certain symptom patterns:

```go
func (a *Agent) PreAnalyze(ctx context.Context, userQuery string, symptoms Symptoms) []BoundaryResult {
    var results []BoundaryResult
    for _, check := range a.boundaryChecks {
        if check.ShouldRun(symptoms) {
            result, err := check.Check(ctx, a.cliCtx, symptoms)
            if err == nil && result.LikelyExternal {
                results = append(results, *result)
            }
        }
    }
    return results
}
```

The LLM system prompt includes instructions to:
1. Run boundary checks when symptoms suggest cross-component issues
2. Present boundary findings before diving into Istio-specific debugging
3. Provide actionable guidance for the external component

## Implementation Steps

1. [ ] Define boundary check interface and types in `context/boundary.go`
2. [ ] Implement CNI boundary detection
3. [ ] Implement DNS/CoreDNS boundary detection
4. [ ] Implement NetworkPolicy boundary detection
5. [ ] Implement kube-proxy boundary detection
6. [ ] Implement certificate/CA boundary detection
7. [ ] Implement symptom detection from user queries
8. [ ] Integrate pre-analysis into agent loop
9. [ ] Write guidance message templates
10. [ ] Write unit tests with mock cluster states
11. [ ] Write integration tests

## Acceptance Criteria

- [ ] Agent correctly identifies when issues may be outside Istio
- [ ] Clear, actionable guidance provided for each external component
- [ ] Boundary checks are fast (< 2 seconds each)
- [ ] False positive rate is low (prefers "might be external" over false certainty)
- [ ] Each boundary check has unit tests
- [ ] Guidance messages include specific kubectl commands users can run
