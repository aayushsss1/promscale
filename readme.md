# PromScale - An LLM-Augmented Kubernetes Operator for Autoscaling Workloads

PromScale is a Kubernetes operator that performs policy-driven autoscaling of workloads using both deterministic, metrics-based rules and an optional LLM-backed decision agent. It integrates with Prometheus for metrics collection and supports advanced scaling behaviors such as stabilization windows, cooldowns, readiness safeguards, and bounded scaling. When enabled, an in-cluster LLM agent can reason over workload signals and operational context to recommend replica counts, with strict validation and deterministic fallbacks to ensure safety and reliability.

## Overview

PromScale extends Kubernetes' native autoscaling capabilities by:
- **Prometheus Integration**: Leverages Prometheus metrics for scaling decisions
- **Dual Decision Modes**: Supports both deterministic rule-based and LLM-powered scaling
- **Flexible Signal Strategies**: Multiple scaling signal types (PerReplica, Threshold)
- **Safety First**: Built-in safeguards, cooldowns, and stabilization windows
- **Production Ready**: Comprehensive status tracking, conditions, and error handling

## Features

### Core Capabilities

- **Prometheus Metrics Integration**: Query Prometheus for custom metrics to drive scaling decisions
- **Multiple Scaling Strategies**:
  - **PerReplica**: Calculate replicas based on target value per replica (e.g., queue depth per pod)
  - **Threshold**: Trigger scaling actions when metrics cross defined thresholds
- **Decision Modes**:
  - **Deterministic**: Rule-based scaling using signal evaluation
  - **LLM**: AI-powered scaling decisions using Google Gemini (with fallback support)
- **Behavior Controls**:
  - Configurable poll intervals
  - Direction-aware cooldowns (separate for scale-up and scale-down)
  - Maximum step size limiting
  - Stabilization windows to prevent flapping
- **Safeguards**:
  - Readiness checks to prevent premature scale-down
  - Missing metrics policies (Hold, ScaleToMin, Error)
  - Min/max replica bounds enforcement
- **Observability**:
  - Comprehensive status tracking
  - Kubernetes conditions for health monitoring
  - Decision history and confidence scores (LLM mode)

## Architecture

<!-- Architecture diagram will be added here -->

PromScale consists of three main components:

1. **Controller** : The Kubernetes operator that reconciles `InferenceScaler` resources
2. **LLM Agent** : Optional FastAPI service that provides AI-powered scaling decisions
3. **Target Workload**: The workload being scaled (currently `apps/v1` Deployment). Support for other workload types (e.g. StatefulSet, custom resources) can be added by extending the operator—implementing target resolution and replica patching for the desired API in the controller code.

## Installation

### Prerequisites

- Kubernetes cluster (v1.24+)
- Prometheus instance accessible from the cluster
- kubectl configured to access your cluster
- Go 1.24+ (for building from source)
- Docker or compatible container runtime

### Quick Start

1. **Install CRDs**:
   ```bash
   make install
   ```

2. **Deploy the Controller**:
   ```bash
   make deploy IMG=<your-registry>/promscale-controller:latest
   ```

3. **Deploy the LLM Agent** (Optional, for LLM mode):
   ```bash
   kubectl create namespace promscale-system
   kubectl create secret generic promscale-agent-secret \
     --from-literal=GOOGLE_API_KEY=<your-api-key> \
     -n promscale-system
   
   # Deploy agent using the Dockerfile in agent/
   kubectl apply -f <agent-deployment-yaml>
   ```

## Usage

### Basic Example

Create an `InferenceScaler` resource to scale a Deployment based on Prometheus metrics:

```yaml
apiVersion: autoscaling.mlop.io/v1alpha1
kind: InferenceScaler
metadata:
  name: my-app-scaler
  namespace: default
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: my-app-deployment
    namespace: default

  minReplicas: 1
  maxReplicas: 10

  prometheus:
    url: http://prometheus.monitoring.svc.cluster.local:9090
    timeoutSeconds: 5

  signals:
    - name: queue-depth
      query: sum(queue_depth_total)
      strategy:
        type: PerReplica
        perReplica:
          target: "5"  # Target 5 items per replica

  decision:
    mode: Deterministic

  behavior:
    pollIntervalSeconds: 15
    scaleUp:
      maxStep: 3
      cooldownSeconds: 10
    scaleDown:
      maxStep: 1
      cooldownSeconds: 30
      stabilizationWindowSeconds: 60

  safeguards:
    missingMetricsPolicy: Hold
    readiness:
      enabled: true
      minReadySecondsAfterScaleUp: 60
```

### LLM-Powered Scaling Example

Enable LLM-based decision making:

```yaml
apiVersion: autoscaling.mlop.io/v1alpha1
kind: InferenceScaler
metadata:
  name: llm-scaler
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: my-app-deployment

  minReplicas: 2
  maxReplicas: 20

  prometheus:
    url: http://prometheus.monitoring.svc.cluster.local:9090

  signals:
    - name: request-rate
      query: rate(http_requests_total[5m])
      strategy:
        type: PerReplica
        perReplica:
          target: "100"
    - name: cpu-utilization
      query: avg(container_cpu_usage_seconds_total)
      strategy:
        type: Threshold
        threshold:
          above: "0.8"
          action:
            type: ScaleUpBy
            scaleUpBy: 2

  decision:
    mode: LLM
    llm:
      endpoint: http://promscale-agent.promscale-system.svc.cluster.local:8081/decide
      timeoutSeconds: 3
      onError: FallbackToDeterministic
      minConfidence: "0.6"

  behavior:
    pollIntervalSeconds: 15
    scaleUp:
      maxStep: 5
      cooldownSeconds: 15
    scaleDown:
      maxStep: 2
      cooldownSeconds: 30
      stabilizationWindowSeconds: 120
```

## Configuration Reference

### InferenceScaler Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetRef` | TargetReference | Yes | Reference to the Deployment to scale |
| `minReplicas` | int32 | No | Minimum number of replicas (default: 1) |
| `maxReplicas` | int32 | Yes | Maximum number of replicas |
| `prometheus` | PrometheusSpec | Yes | Prometheus configuration |
| `signals` | []ScalingSignal | Yes | List of scaling signals to evaluate |
| `decision` | DecisionSpec | No | Decision mode configuration (default: Deterministic) |
| `behavior` | ScalingBehavior | No | Scaling behavior configuration |
| `safeguards` | SafeguardsSpec | No | Safety guardrails |

### Scaling Signals

#### PerReplica Strategy

Calculates replicas based on a target value per replica:

```yaml
signals:
  - name: queue-depth
    query: sum(queue_depth_total)
    strategy:
      type: PerReplica
      perReplica:
        target: "5"  # Desired value per replica
```

**Formula**: `replicas = ceil(metric_value / target)`

#### Threshold Strategy

Triggers scaling actions when metrics cross thresholds:

```yaml
signals:
  - name: high-cpu
    query: avg(container_cpu_usage_seconds_total)
    strategy:
      type: Threshold
      threshold:
        above: "0.8"  # Trigger if metric > 0.8
        action:
          type: ScaleUpBy
          scaleUpBy: 2
```

**Actions**:
- `ScaleUpBy`: Increase replicas by N
- `ScaleDownBy`: Decrease replicas by N
- `Noop`: No action (useful for monitoring)

### Decision Modes

#### Deterministic Mode

Uses rule-based evaluation of signals:
- PerReplica signals compute desired replicas using `ceil(metric / target)`
- Threshold signals apply additive deltas
- Final desired replicas = max(PerReplica recommendations) + sum(Threshold deltas)

#### LLM Mode

Delegates scaling decisions to an LLM agent:
- Controller sends current state and signals to agent
- Agent returns desired replicas, reason, and confidence
- Falls back to deterministic mode on errors (configurable)

**LLM Agent Requirements**:
- FastAPI endpoint at `/decide`
- Accepts JSON request with scaler state and signals
- Returns JSON with `desiredReplicas`, `reason`, and `confidence`

### Behavior Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pollIntervalSeconds` | int32 | 15 | How often to evaluate metrics |
| `scaleUp.maxStep` | int32 | - | Maximum replicas to add per step |
| `scaleUp.cooldownSeconds` | int32 | - | Minimum seconds between scale-ups |
| `scaleDown.maxStep` | int32 | - | Maximum replicas to remove per step |
| `scaleDown.cooldownSeconds` | int32 | - | Minimum seconds between scale-downs |
| `scaleDown.stabilizationWindowSeconds` | int32 | - | Use max recommendation in this window |

### Safeguards

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `missingMetricsPolicy` | string | Hold | Action when Prometheus unavailable: Hold, ScaleToMin, Error |
| `readiness.enabled` | bool | true | Prevent scale-down shortly after scale-up |
| `readiness.minReadySecondsAfterScaleUp` | int32 | 60 | Seconds to wait before allowing scale-down after scale-up |

## Status and Observability

The `InferenceScaler` resource exposes comprehensive status information:

```yaml
status:
  observedGeneration: 1
  lastPollTime: "2026-01-15T10:30:00Z"
  lastScaleTime: "2026-01-15T10:25:00Z"
  currentReplicas: 5
  readyReplicas: 5
  desiredReplicas: 6
  signals:
    - name: queue-depth
      value: "28.5"
      timestamp: "2026-01-15T10:30:00Z"
  recommendations:
    - timestamp: "2026-01-15T10:30:00Z"
      desiredReplicas: 6
  conditions:
    - type: ScalingActive
      status: "True"
      reason: scaled
      message: "Scaled Deployment to 6"
    - type: MetricsAvailable
      status: "True"
      reason: prometheus_ready
    - type: LLMAvailable
      status: "True"
      reason: agent_ok
  lastDecision:
    mode: LLM
    source: agent
    reason: "High queue depth detected, scaling up to handle load"
    confidence: "0.85"
    time: "2026-01-15T10:30:00Z"
```

### Conditions

- `ScalingActive`: Whether scaling is currently active
- `MetricsAvailable`: Prometheus connectivity status
- `SignalsEvaluated`: Signal evaluation status
- `LLMAvailable`: LLM agent availability (LLM mode only)
- `ScalingLimited`: Whether scaling was limited by bounds/rails

## Development

### Building from Source

```bash
# Build the controller binary
make build

# Build Docker image
make docker-build IMG=promscale-controller:latest

# Generate manifests
make manifests
```

### Project Structure

```
promscale/
├── api/v1alpha1/          # CRD API definitions
│   ├── inferencescaler_types.go
│   └── groupversion_info.go
├── cmd/main.go            # Controller entry point
├── internal/controller/   # Controller logic
│   ├── inferencescaler_controller.go
│   ├── prometheus.go      # Prometheus client
│   ├── agent.go          # LLM agent client
│   ├── signals.go        # Signal evaluation
│   ├── behaviour.go      # Behavior application
│   └── status.go         # Status management
├── agent/                # LLM Agent (Python)
│   ├── agent.py         # FastAPI service
│   └── requirements.txt
├── app/                  # Mock app for testing
│   ├── app.py           # FastAPI with Prometheus metrics
│   └── requirements.txt
├── config/               # Kubernetes manifests
│   ├── crd/             # CRD definitions
│   ├── rbac/            # RBAC configurations
│   └── manager/         # Controller deployment
```

### Running Locally

```bash
# Install CRDs
make install

# Run controller locally (requires kubeconfig)
make run

# In another terminal, apply a sample InferenceScaler
kubectl apply -f inference-scaler.yaml
```

### LLM Agent Development

The LLM agent is a Python FastAPI service. To run locally:

```bash
cd agent
python -m venv env
source env/bin/activate
pip install -r requirements.txt
export GOOGLE_API_KEY=<your-key>
uvicorn agent:app --host 0.0.0.0 --port 8081
```

## Troubleshooting

### Controller Not Scaling

1. Check controller logs:
   ```bash
   kubectl logs -n promscale-system deployment/promscale-controller-manager
   ```

2. Verify InferenceScaler status:
   ```bash
   kubectl describe inferencescaler <name> -n <namespace>
   ```

3. Check conditions:
   ```bash
   kubectl get inferencescaler <name> -o jsonpath='{.status.conditions}'
   ```

### Prometheus Connection Issues

- Verify Prometheus URL is accessible from the cluster
- Check network policies if using them
- Ensure Prometheus is ready: `curl <prometheus-url>/-/ready`

### LLM Agent Issues

- Verify agent endpoint is reachable from controller pods
- Check agent logs for errors
- Ensure `GOOGLE_API_KEY` is set correctly
- Review `lastDecision.agentError` in InferenceScaler status