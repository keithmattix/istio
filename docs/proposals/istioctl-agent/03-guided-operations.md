# Sub-issue 3: Guided Operations Framework

**Labels**: `enhancement`, `area/istioctl`  
**Parent**: [Epic] istioctl AI Troubleshooting Agent

## Summary

Implement a guided operations framework that helps users perform complex Istio operations through an interactive, question-and-answer workflow. Each operation examines the current cluster state, asks clarifying questions, generates configuration, and applies it with user confirmation.

## Motivation

Many Istio operations require understanding multiple configuration options and their interactions. Users often don't know what questions to ask. The guided operations framework turns complex multi-step operations into interactive wizards that the agent can drive.

## Detailed Design

### Operation Interface

```go
// Operation represents a guided, multi-step Istio operation
type Operation struct {
    Name        string
    Description string
    Category    string   // "ambient", "traffic", "security", "install"
    Trigger     string   // Natural language trigger description for the LLM
    Steps       []Step
    Execute     func(ctx context.Context, answers map[string]interface{}, cliCtx cli.Context) (*OperationResult, error)
}

// Step represents a single step in a guided operation
type Step struct {
    ID          string
    Question    string                    // Question to ask the user
    Type        StepType                  // Choice, FreeText, Confirm, Info
    Options     []Option                  // For Choice type
    Default     interface{}               // Default value
    Validate    func(interface{}) error   // Input validation
    Condition   func(answers map[string]interface{}) bool // Whether to show this step
    DependsOn   []string                  // Step IDs this depends on
}

type StepType int
const (
    StepChoice   StepType = iota // Multiple choice
    StepFreeText                  // Free text input
    StepConfirm                   // Yes/No confirmation
    StepInfo                      // Informational, no input needed
)

// OperationResult contains the outcome of an operation
type OperationResult struct {
    GeneratedYAML string   // YAML that was/would be applied
    Applied       bool     // Whether it was actually applied
    Messages      []string // Status messages
    NextSteps     []string // Suggested follow-up actions
}
```

### Initial Operations

#### 1. Waypoint Deployment Wizard

**Trigger**: "help me deploy a waypoint", "set up waypoint proxy", "configure L7 processing for ambient"

**Cluster Examination** (automatic):
- Check if namespace is enrolled in ambient mode
- Check if waypoints already exist
- List services in the namespace
- Check Gateway API CRD availability

**Steps**:

| Step | Question | Type | Options |
|---|---|---|---|
| 1 | (Info) Current cluster state analysis | Info | — |
| 2 | What granularity should the waypoint have? | Choice | per-namespace, per-service |
| 3 | (If per-service) Which service(s)? | Choice | [discovered services] |
| 4 | What traffic type should the waypoint handle? | Choice | service (L7 for service traffic), workload (L7 for workload traffic), all |
| 5 | Custom waypoint name? | FreeText | Default: "waypoint" |
| 6 | Should the namespace be auto-enrolled to use this waypoint? | Confirm | Default: Yes |
| 7 | (Info) Generated configuration preview | Info | — |
| 8 | Apply this configuration to the cluster? | Confirm | — |
| 9 | (If applied) Wait for waypoint to be ready? | Confirm | Default: Yes |

**Implementation notes**:
- Uses `istioctl/pkg/waypoint/` functions for generation and application
- Validates namespace has `istio.io/dataplane-mode=ambient` label
- Warns if Gateway API CRDs are missing (IST0156)
- Uses existing `waypoint generate` and `waypoint apply` logic

#### 2. mTLS Migration Wizard

**Trigger**: "help me enable strict mTLS", "migrate to mTLS", "secure my mesh"

**Cluster Examination**:
- Check current PeerAuthentication policies
- Check DestinationRule TLS settings
- Identify namespaces with/without sidecars or ambient enrollment
- Check for non-mesh workloads that communicate with mesh workloads

**Steps**:

| Step | Question | Type | Options |
|---|---|---|---|
| 1 | (Info) Current mTLS status across namespaces | Info | — |
| 2 | Scope of migration? | Choice | mesh-wide, per-namespace, per-workload |
| 3 | (If per-namespace) Which namespace? | Choice | [discovered namespaces] |
| 4 | (Info) Identified non-mesh workloads that may break | Info | — |
| 5 | Migration strategy? | Choice | Direct (STRICT immediately), Gradual (PERMISSIVE → monitor → STRICT) |
| 6 | (If gradual) How long to monitor before switching? | FreeText | Default: "24h" |
| 7 | (Info) Generated PeerAuthentication YAML | Info | — |
| 8 | Apply this configuration? | Confirm | — |

#### 3. Ambient Mode Migration Wizard

**Trigger**: "migrate from sidecar to ambient", "enable ambient mode", "switch to ambient"

**Cluster Examination**:
- Check current data plane mode per namespace
- Identify pods with sidecars
- Check Istio version supports ambient
- Check CNI plugin configuration

**Steps**:

| Step | Question | Type | Options |
|---|---|---|---|
| 1 | (Info) Current data plane mode analysis | Info | — |
| 2 | Migration scope? | Choice | Entire cluster, Specific namespaces |
| 3 | (If specific) Which namespaces? | Choice | [discovered namespaces with sidecars] |
| 4 | Need L7 features (routing, retries, etc.)? | Confirm | — |
| 5 | (If yes) Deploy waypoint proxies? | Confirm | — |
| 6 | (Info) Migration plan with steps | Info | — |
| 7 | Proceed with migration? | Confirm | — |

**Post-apply actions**:
- Label namespace with `istio.io/dataplane-mode=ambient`
- Remove `istio-injection=enabled` label
- Restart pods to remove sidecars
- Optionally deploy waypoint

#### 4. Traffic Management Wizard

**Trigger**: "set up canary deployment", "configure traffic splitting", "route traffic based on headers"

**Cluster Examination**:
- List services and their versions/subsets
- Check existing VirtualServices and DestinationRules
- Identify deployment strategies

**Steps**:

| Step | Question | Type | Options |
|---|---|---|---|
| 1 | What type of traffic management? | Choice | Canary (weight-based), A/B testing (header-based), Traffic mirroring, Circuit breaking |
| 2 | Which service? | Choice | [discovered services] |
| 3 | (Canary-specific steps for weights, versions) | Various | — |
| 4 | (A/B-specific steps for header matching) | Various | — |
| 5 | (Info) Generated VirtualService/DestinationRule YAML | Info | — |
| 6 | Apply? | Confirm | — |

#### 5. AuthorizationPolicy Wizard

**Trigger**: "set up authorization policy", "restrict access to service", "configure RBAC"

**Cluster Examination**:
- List existing AuthorizationPolicies
- Identify service accounts in the namespace
- Check current PeerAuthentication (mTLS required for principal-based auth)

**Steps**:

| Step | Question | Type | Options |
|---|---|---|---|
| 1 | Policy action? | Choice | ALLOW specific traffic, DENY specific traffic, CUSTOM (external authz) |
| 2 | Apply to which workload(s)? | Choice | All in namespace, Specific workload |
| 3 | (If specific) Which workload? | Choice | [discovered workloads] |
| 4 | Source restrictions? | Choice | By namespace, By service account, By IP range, No restriction |
| 5 | Path/method restrictions? | Choice | Specific paths, Specific methods, Both, None |
| 6 | (Info) Generated AuthorizationPolicy YAML | Info | — |
| 7 | Apply? | Confirm | — |

### Operation Registry

```go
type OperationRegistry struct {
    operations map[string]*Operation
}

func NewDefaultRegistry() *OperationRegistry {
    r := &OperationRegistry{}
    r.Register(NewWaypointDeployOperation())
    r.Register(NewMTLSMigrationOperation())
    r.Register(NewAmbientMigrationOperation())
    r.Register(NewTrafficManagementOperation())
    r.Register(NewAuthzPolicyOperation())
    return r
}
```

### Safety Guarantees

1. **Every mutating step requires explicit user confirmation**
2. **Generated YAML is always shown before applying**
3. **`--dry-run` mode generates YAML but never applies**
4. **Rollback information provided** (how to undo each operation)
5. **Pre-flight validation** before applying (similar to `istioctl analyze`)

### Agent Integration

The agent LLM detects when a user's request matches a guided operation trigger and switches from the general troubleshooting ReAct loop to the guided operation flow. The operation drives the conversation through its steps, collecting answers, and then generates and optionally applies configuration.

## Implementation Steps

1. [ ] Define operation interface and types in `operations/operation.go`
2. [ ] Implement `operations/registry.go`
3. [ ] Implement waypoint deployment wizard
4. [ ] Implement mTLS migration wizard
5. [ ] Implement ambient mode migration wizard
6. [ ] Implement traffic management wizard
7. [ ] Implement authorization policy wizard
8. [ ] Integrate operation detection into agent loop
9. [ ] Add YAML preview and confirmation flow
10. [ ] Write unit tests for each operation with mock cluster state
11. [ ] Write integration tests with real cluster interactions

## Acceptance Criteria

- [ ] Each operation walks through its steps in order, respecting conditions
- [ ] Cluster state is examined before presenting options
- [ ] All mutating actions require explicit confirmation
- [ ] Generated YAML is valid and can be applied with kubectl
- [ ] `--dry-run` mode works for all operations
- [ ] Operations provide rollback instructions
- [ ] New operations can be added by implementing the Operation interface
