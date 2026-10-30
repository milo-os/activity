# Activity System Observability

> Last verified: 2026-02-25 against production readiness implementation

This guide covers the observability features of the Activity system: health probes, metrics, alerts, and dashboards.

## Health Probes

All Activity components expose HTTP health endpoints for Kubernetes liveness and readiness probes.

### Probe Endpoints

| Component | Liveness | Readiness | Port |
|-----------|----------|-----------|------|
| activity-processor | `/healthz` | `/readyz` | 8081 |
| k8s-event-exporter | `/healthz` | `/readyz` | 8081 |
| activity-controller-manager | `/healthz` | `/readyz` | 8081 |
| activity-apiserver | `/healthz` | `/readyz` | 6443 |

### Probe Configuration

Health probes are configured in each component's deployment manifest.

#### activity-processor

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: health
    scheme: HTTP
  initialDelaySeconds: 15
  periodSeconds: 20
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /readyz
    port: health
    scheme: HTTP
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3
```

**Liveness checks:**
- NATS connection is established

**Readiness checks:**
- NATS connection is healthy
- Policy cache has synced
- At least one ActivityPolicy is ready (warning if zero)

**Rationale:** Liveness threshold higher than readiness to prevent restart loops during temporary NATS disconnections. Processor has automatic reconnection logic.

#### k8s-event-exporter

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: health
    scheme: HTTP
  initialDelaySeconds: 15
  periodSeconds: 20
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /readyz
    port: health
    scheme: HTTP
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3
```

**Liveness checks:**
- NATS connection is established

**Readiness checks:**
- NATS connection is healthy
- Kubernetes event informer cache has synced

#### activity-controller-manager

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: health
    scheme: HTTP
  initialDelaySeconds: 15
  periodSeconds: 20

readinessProbe:
  httpGet:
    path: /readyz
    port: health
    scheme: HTTP
  initialDelaySeconds: 5
  periodSeconds: 10
```

**Liveness checks:**
- Controller runtime is running

**Readiness checks:**
- Informer caches have synced
- Leader election complete (if enabled)

Uses controller-runtime default health checks.

### Testing Health Probes

Test health endpoints manually:

```bash
# Get pod name
POD=$(kubectl get pod -n activity-system -l app=activity-processor -o jsonpath='{.items[0].metadata.name}')

# Test liveness
kubectl exec -n activity-system $POD -- wget -qO- http://localhost:8081/healthz

# Test readiness
kubectl exec -n activity-system $POD -- wget -qO- http://localhost:8081/readyz
```

Expected response: `200 OK` with body `ok` when healthy, `503 Service Unavailable` with error message when unhealthy.

### Probe Failure Behavior

**Liveness probe failure:**
- Pod is restarted by kubelet after `failureThreshold` consecutive failures
- Use for detecting deadlocks or unrecoverable errors
- Should almost never fail if readiness checks pass

**Readiness probe failure:**
- Pod removed from service endpoints
- No traffic sent to pod
- Pod continues running (not restarted)
- Use for temporary unavailability (NATS disconnected, cache syncing)

## Prometheus Metrics

All components expose Prometheus metrics on their health/metrics port.

### activity-processor Metrics

Exposed on port 8081 at `/metrics` endpoint.

#### Event Processing Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `activity_processor_events_received_total` | counter | `source`, `api_group`, `resource` | Events received from NATS (`source` is `audit_log` or `control_plane_event`) |
| `activity_processor_events_evaluated_total` | counter | `source`, `policy_name`, `api_group`, `kind`, `matched` | Events evaluated against policies |
| `activity_processor_events_skipped_total` | counter | `source`, `reason` | Events skipped during processing |
| `activity_processor_events_errored_total` | counter | `source`, `error_type` | Events that failed processing |
| `activity_processor_activities_generated_total` | counter | `policy_name`, `api_group`, `kind` | Activities successfully generated |
| `activity_processor_event_processing_duration_seconds` | histogram | `source`, `policy_name` | Time to process an event |

**Skip reasons:**
- `no_matching_policy` - No policy matched the event
- `no_audit_rules` - Policy has no audit rules defined
- `invalid_event` - Event failed validation

**Error types:**
- `json_unmarshal` - Failed to parse event JSON
- `cel_evaluation` - CEL expression evaluation error
- `nats_publish` - Failed to publish activity to NATS
- `activity_creation` - Failed to create activity object

#### Policy Cache Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `activity_processor_active_policies` | gauge | - | Number of ActivityPolicies loaded |

#### Worker Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `activity_processor_active_workers` | gauge | - | Number of active worker goroutines |

#### NATS Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `activity_processor_nats_connection_status` | gauge | - | NATS connection status (1=connected, 0=disconnected) |
| `activity_processor_nats_disconnects_total` | counter | - | Total NATS disconnection events |
| `activity_processor_nats_reconnects_total` | counter | - | Total NATS reconnection events |
| `activity_processor_nats_errors_total` | counter | - | Total NATS errors |
| `activity_processor_nats_messages_published_total` | counter | - | Total messages published to NATS |
| `activity_processor_nats_publish_latency_seconds` | histogram | - | NATS publish operation latency |

### k8s-event-exporter Metrics

Exposed on port 8081 at `/metrics` endpoint.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `event_exporter_events_published_total` | counter | `namespace`, `reason` | Kubernetes events published to NATS |
| `event_exporter_publish_errors_total` | counter | - | Event publish errors |
| `event_exporter_informer_synced` | gauge | - | Whether informer cache is synced (1=synced, 0=not synced) |
| `event_exporter_nats_connection_status` | gauge | - | NATS connection status (1=connected, 0=disconnected) |
| `event_exporter_publish_latency_seconds` | histogram | - | NATS publish operation latency |

### activity-controller-manager Metrics

Exposed on port 8080 at `/metrics` endpoint.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `activity_controller_policies_validated_total` | counter | `result` | ActivityPolicy validation attempts |
| `activity_controller_policy_validation_errors_total` | counter | `error_type` | Policy validation errors by type |
| `activity_controller_policy_ready` | gauge | `policy`, `reason` | Policy Ready condition status |
| `activity_controller_reconcile_duration_seconds` | histogram | - | Policy reconciliation duration |

**Validation results:**
- `success` - Policy passed validation
- `failed` - Policy failed validation
- `resource_not_found` - Referenced resource doesn't exist

**Validation error types:**
- `cel_compilation` - CEL expression syntax error
- `resource_lookup` - apiGroup/kind not found
- `rule_validation` - Rule structure invalid

Additionally, controller-runtime provides default metrics:
- `controller_runtime_reconcile_total` - Total reconciliations
- `controller_runtime_reconcile_errors_total` - Reconciliation errors
- `workqueue_depth` - Work queue size
- `workqueue_adds_total` - Items added to work queue

### Querying Metrics

Access metrics via Prometheus or directly from pods:

```bash
# Port-forward to Prometheus
kubectl port-forward -n monitoring svc/prometheus 9090:9090

# Query in Prometheus UI at http://localhost:9090

# Or query metrics directly from pod
kubectl exec -n activity-system <pod-name> -- wget -qO- http://localhost:8081/metrics
```

Example queries:

```promql
# Event processing rate (all sources)
rate(activity_processor_events_received_total[5m])

# Audit log event processing rate only
rate(activity_processor_events_received_total{source="audit_log"}[5m])

# Activity generation rate
rate(activity_processor_activities_generated_total[5m])

# Error rate percentage (all sources)
sum(rate(activity_processor_events_errored_total[5m]))
/
sum(rate(activity_processor_events_received_total[5m]))
* 100

# Processing latency p99
histogram_quantile(0.99,
  sum(rate(activity_processor_event_processing_duration_seconds_bucket[5m])) by (le)
)

# Active policies
activity_processor_active_policies

# NATS connection status
activity_processor_nats_connection_status
```

## Alerts

Activity system alerts are defined in PrometheusRule resources and managed by Prometheus Alertmanager.

### Alert Configuration

Alerts are defined in `config/components/observability/alerts/activity-alerts.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: activity-alerts
  namespace: activity-system
spec:
  groups:
    - name: activity-sli
    - name: activity-pipeline
    - name: activity-processor
    - name: activity-controller
```

### Alert Groups

#### SLI Alerts (activity-sli)

User-facing service quality alerts:

- **ActivityAPIServerDown** - API server unavailable for 5+ minutes (critical)
- **ActivityHighErrorRate** - >1% requests failing for 10+ minutes (warning)
- **ActivityQueryLatencyHigh** - p99 query latency >10s for 10+ minutes (warning)
- **ActivityClickHouseUnavailable** - ClickHouse connection errors for 5+ minutes (critical)

#### Pipeline Alerts (activity-pipeline)

Data freshness and event processing alerts:

- **ActivityDataPipelineStalled** - No events written to ClickHouse for 15+ minutes (critical)
- **ActivityPipelineBacklogCritical** - NATS consumer lag >500k messages for 10+ minutes (critical)
- **EventExporterDown** - Event exporter unavailable for 5+ minutes (warning)
- **EventExporterPublishErrors** - >0.1 publish errors/sec for 10+ minutes (warning)

#### Processor Alerts (activity-processor)

Translation engine health alerts:

- **ActivityProcessorDown** - Processor unavailable for 5+ minutes (critical)
- **ActivityProcessorNATSDisconnected** - Lost NATS connection for 2+ minutes (critical)
- **ActivityGenerationStalled** - Receiving audit events but evaluating none against policies for 15+ minutes (warning)
- **ActivityProcessorHighErrorRate** - >5% error rate for 10+ minutes (warning)
- **ActivityProcessorNoPolicies** - No active policies for 15+ minutes (warning)

#### Sidecar Alerts (vector-sidecar)

Audit collection from milo-apiserver to NATS, evaluated per pod:

- **VectorSidecarNATSPublishStalled** - A sidecar is transforming events but its NATS sink has consumed none for 5+ minutes (critical)
- **VectorSidecarBufferBacklog** - A sidecar has >25k events buffered on disk for 15+ minutes (warning)

#### Controller Alerts (activity-controller)

ActivityPolicy lifecycle alerts:

- **ActivityPolicyValidationFailing** - Validation errors occurring for 15+ minutes (warning)
- **ActivityPolicyNotReady** - Policies stuck in non-ready state for 30+ minutes (warning)

### Alert Annotations

Every alert includes:

- **summary** - One-line description
- **description** - Details about the condition
- **impact** - What users experience

Example:

```yaml
annotations:
  summary: "Activity processor is unavailable"
  description: "Activity processor has been down for more than 5 minutes. No new activities are being generated."
  impact: "Complete activity generation outage - audit events not translated to activities"
```

### Viewing Alerts

Check firing alerts in Prometheus Alertmanager:

```bash
# Port-forward to Alertmanager
kubectl port-forward -n monitoring svc/alertmanager 9093:9093

# Open http://localhost:9093 in browser
```

Or query alert state in Prometheus:

```promql
# Show all firing alerts
ALERTS{alertstate="firing"}

# Show specific alert
ALERTS{alertname="ActivityProcessorDown"}
```

### Testing Alerts

Verify alert expressions work correctly:

```bash
# Query Prometheus to test alert condition
# Example: Test if processor down alert would fire
up{job="activity-processor"} == 0
```

Simulate failure conditions:

```bash
# Scale processor to 0 to test ActivityProcessorDown
kubectl scale deployment/activity-processor -n activity-system --replicas=0

# Wait 5 minutes, alert should fire
# Restore
kubectl scale deployment/activity-processor -n activity-system --replicas=1
```

## Grafana Dashboards

The Activity system includes multiple Grafana dashboards for monitoring different aspects of the system. Use the overview dashboard for quick health checks, then drill into specific component dashboards for detailed investigation.

### Dashboard Overview

| Dashboard | Purpose | When to Use |
|-----------|---------|-------------|
| Activity System Overview | Single-pane-of-glass health check for entire system | First stop for health checks, SRE triage |
| Events Pipeline | K8s event collection and storage | Investigating events not appearing in ClickHouse |
| Activity Processor | Activity translation and policy evaluation | Existing processor dashboard for audit events |
| Audit Log Pipeline | Audit log processing through Vector | Investigating audit log pipeline issues |

### Accessing Dashboards

```bash
# Port-forward to Grafana
kubectl port-forward -n monitoring svc/grafana 3000:3000

# Open http://localhost:3000/dashboards
# Navigate to desired dashboard
```

Or access via Grafana URL in your environment.

## Activity System Overview Dashboard

Single-pane-of-glass health view for the entire Activity system. Start here for triage and navigate to detailed dashboards as needed.

### Dashboard Purpose

This dashboard provides:
- At-a-glance health status for all major components
- Combined throughput metrics across all pipelines
- System-wide error rates
- Quick links to detailed component dashboards

**Use this dashboard for:**
- Initial health checks during incidents
- SRE monitoring and on-call triage
- Capacity planning (overall throughput trends)
- Identifying which component needs deeper investigation

### Dashboard Layout

The dashboard is organized into four rows.

#### Row 1: System Health

Four health status indicators showing component availability:

- **Audit Pipeline Status** - Vector up and receiving audit events (Healthy/Degraded)
- **Events Pipeline Status** - Event exporter connected to NATS and informer synced (Healthy/Degraded)
- **Activity Generation Status** - Processor up and connected to NATS (Healthy/Degraded)
- **Storage Health** - ClickHouse and NATS backend availability (All Up/Down)

**Thresholds:**
- Green (Healthy/All Up) = Component fully operational
- Red (Degraded/Down) = Component unavailable or not functioning

**What to look for:**
- All four should be green in healthy state
- Any red status requires immediate investigation
- Check detailed component dashboard for degraded services

#### Row 2: Throughput Summary

Four stat panels showing current processing rates:

- **Audit Events/sec** - Audit log processing rate
- **K8s Events/sec** - Kubernetes event collection rate
- **Activities Generated/sec** - Activity generation rate from policies
- **Total Storage Rate** - Combined ClickHouse insert rate across all tables

**What to look for:**
- Rates should correlate with cluster activity
- Zero rates may indicate pipeline stalls (check health indicators)
- Sudden drops suggest downstream bottlenecks

#### Row 3: Key Metrics

Three time series panels showing trends over time:

- **Combined Pipeline Throughput** - Stacked area chart of audit events, K8s events, and activities
- **Error Rates Across All Components** - Stacked area chart of errors from all pipelines
- **End-to-End Latency (p95)** - Latency for audit and events pipelines

**What to look for:**
- Throughput should track cluster activity patterns
- Error rates should remain near zero in healthy state
- Latency spikes indicate processing bottlenecks

#### Row 4: Quick Links

Text panel with links to detailed dashboards and runbooks. Use these to drill into specific components for deeper investigation.

### Using the Dashboard

#### Quick Health Check

1. Check **System Health** row - all should be green
2. Verify **Throughput Summary** rates are non-zero
3. Check **Error Rates** panel - should be near zero
4. If issues found, click appropriate detailed dashboard link

#### Investigating Degraded Health

**If Audit Pipeline Status is red:**
1. Click "Audit Log Pipeline" dashboard link
2. Check Vector metrics for connection issues
3. Review NATS stream health

**If Events Pipeline Status is red:**
1. Click "Events Pipeline" dashboard link
2. Check event exporter connection status
3. Verify informer sync status

**If Activity Generation Status is red:**
1. Click "Activity Processor" dashboard link
2. Check NATS connection metrics
3. Review active policies count

**If Storage Health is red:**
1. Check ClickHouse pod status: `kubectl get pods -n activity-system -l app=clickhouse`
2. Check NATS pod status: `kubectl get pods -n nats-system`
3. Review component logs for connection errors

#### Investigating Throughput Issues

**If rates drop to zero:**
1. Check corresponding health indicator in Row 1
2. Navigate to detailed dashboard for that component
3. Look for connection or processing errors

**If rates are lower than expected:**
1. Review **Combined Pipeline Throughput** for trend patterns
2. Check **End-to-End Latency** for processing slowdowns
3. Verify cluster is generating expected events (kubectl events)

#### Investigating Errors

1. View **Error Rates Across All Components** to identify which pipeline has errors
2. Click link to detailed dashboard for that component
3. Review error type breakdown in component dashboard
4. Check component logs for specific error messages

## Events Pipeline Dashboard

End-to-end monitoring of Kubernetes events pipeline: k8s-event-exporter → NATS → Vector → ClickHouse.

### Dashboard Purpose

This dashboard tracks the flow of Kubernetes cluster events through the entire pipeline from collection to storage. It monitors the event exporter's health, NATS publishing performance, Vector routing, and ClickHouse write path.

**Use this dashboard for:**
- Troubleshooting missing events in ClickHouse
- Investigating event collection issues
- Monitoring event processing performance
- Debugging pipeline backpressure

### Dashboard Layout

The dashboard is organized into five rows with 16 panels.

#### Row 1: Critical Health Indicators

Seven stat panels providing at-a-glance health status:

- **Events Published Rate** - Events/sec published by k8s-event-exporter to NATS
- **Events Written Rate** - Events/sec written to ClickHouse k8s_events table
- **Queue Backlog** - Pending events in NATS queue (backpressure indicator)
- **Error Rate** - Combined errors across event exporter and Vector
- **Exporter Connection Status** - NATS connection health (Connected/Disconnected)
- **Informer Sync Status** - K8s informer cache sync status (Synced/Not Synced)
- **ClickHouse Insert Latency** - Average time to write events to k8s_events table

**Thresholds:**

Events Published Rate:
- Green >1 events/sec (healthy cluster activity)
- Yellow 0.1-1 events/sec (low activity)
- Red <0.1 events/sec (possible issue)

Events Written Rate:
- Green >1 events/sec (healthy writes)
- Yellow 0.1-1 events/sec (low writes)
- Red <0.1 events/sec (write issues)

Queue Backlog:
- Green <100 pending (normal lag)
- Yellow 100-1000 pending (backpressure building)
- Red >1000 pending (significant backlog)

Error Rate:
- Green 0 errors/sec (healthy)
- Yellow 0.1-1 errors/sec (intermittent issues)
- Red >1 error/sec (critical issues)

ClickHouse Insert Latency:
- Green <100ms (fast writes)
- Yellow 100-500ms (moderate slowdown)
- Orange 500ms-1s (significant slowdown)
- Red >1s (critical slowdown)

**What to look for:**
- Published Rate should roughly equal Written Rate in steady state
- Queue Backlog increasing indicates downstream bottleneck
- Connection/Sync status must be green for events to flow
- High insert latency suggests ClickHouse performance issues

#### Row 2: Event Exporter

Three time series panels showing event collection details:

- **Events by Namespace** - Top 10 namespaces generating events (stacked area)
- **Events by Reason** - Common event reasons like Created, Scheduled, Pulling (stacked area)
- **Publish Latency (p50/p95/p99)** - Latency distribution for publishing events to NATS

**What to look for:**
- High-volume namespaces may indicate specific workload issues
- Event reason breakdown shows normal cluster operations patterns
- Publish latency should remain <100ms; spikes indicate NATS issues

#### Row 3: Pipeline Flow

Two time series panels showing data flow:

- **Event Flow Through Stages** - Events/sec at each pipeline stage (should track together)
  - 1. Published to NATS
  - 2. Consumed from NATS
  - 3. Sent to ClickHouse
  - 4. ClickHouse Writes
- **Ingress vs Egress Comparison** - Pipeline input vs output (gap indicates bottleneck)

**What to look for:**
- All four stages should track together in steady state
- Gaps between stages indicate buffering or processing delays
- Persistent gap between ingress and egress indicates data loss or backlog

#### Row 4: Performance

Four time series panels showing performance metrics:

- **NATS Consumer Lag** - Pending messages for events consumer (backpressure indicator)
- **Vector Buffer Depth** - Buffered events waiting for ClickHouse write
- **ClickHouse Insert Performance** - Events insert rate over time
- **Publish Errors Over Time** - Event exporter errors publishing to NATS (should be zero)

**What to look for:**
- Rising consumer lag means Vector can't keep up with event rate
- High buffer depth indicates ClickHouse write slowness
- Insert rate dropping suggests ClickHouse performance degradation
- Any publish errors require immediate investigation

#### Row 5: Error Breakdown (collapsed)

Two time series panels showing error details:

- **Errors by Component** - Breakdown of errors: Exporter vs Vector (stacked area)
- **Vector Component Errors** - Detailed Vector pipeline errors for events stream

**What to look for:**
- Identify whether errors originate from exporter or Vector
- Vector component errors show which pipeline stage is failing
- Check component logs for specific error messages

### Using the Dashboard

#### Investigating Missing Events

**Symptom:** Events visible in cluster but not appearing in ClickHouse.

1. Check **Exporter Connection Status** - must show "Connected"
2. Check **Informer Sync Status** - must show "Synced"
3. View **Events Published Rate** - should be >0 if cluster has activity
4. Check **Event Flow Through Stages** - identify which stage drops events
5. If published but not written, check **Vector Buffer Depth** and **ClickHouse Insert Performance**

#### Investigating Pipeline Backlog

**Symptom:** Queue Backlog or Consumer Lag increasing.

1. Check **Queue Backlog** and **NATS Consumer Lag** - quantify backlog size
2. View **Ingress vs Egress Comparison** - confirm ingress exceeds egress
3. Check **Vector Buffer Depth** - if high, ClickHouse is the bottleneck
4. Review **ClickHouse Insert Latency** - if high, investigate ClickHouse performance
5. Check ClickHouse pod CPU/memory usage

#### Investigating Publish Errors

**Symptom:** Error Rate >0 or Publish Errors increasing.

1. View **Publish Errors Over Time** - confirm errors are occurring
2. Check **Errors by Component** - identify if exporter or Vector errors
3. Review event exporter logs:
   ```bash
   kubectl logs -n activity-system -l app=k8s-event-exporter --tail=100
   ```
4. Common causes:
   - NATS connection issues (check Connection Status)
   - NATS stream full (check NATS stream limits)
   - Event serialization errors (check logs for malformed events)

#### Investigating Slow Processing

**Symptom:** High latency or slow event flow.

1. Check **ClickHouse Insert Latency** - if >1s, ClickHouse is slow
2. Review **Vector Buffer Depth** - if increasing, writes are backing up
3. Check **Publish Latency** - if high, NATS publishing is slow
4. View **Event Flow Through Stages** - identify which stage has gaps
5. Investigate bottleneck component performance

## Activity Processor Dashboard

The Activity Processor dashboard provides real-time visibility into event processing, policy evaluation, and NATS health.

### Accessing the Dashboard

```bash
# Port-forward to Grafana
kubectl port-forward -n monitoring svc/grafana 3000:3000

# Open http://localhost:3000/dashboards
# Navigate to "Activity Processor - Event Pipeline"
```

Or access via Grafana URL in your environment.

### Activity Processor Dashboard Overview

The dashboard is organized into four rows with collapsible sections.

#### Row 1: Overview Stats

Quick health indicators:

- **Event Processing Rate** - Events/sec received from NATS
- **Activity Generation Rate** - Activities/sec generated and published
- **Error Rate** - Percentage of events failing processing (threshold: <5%)
- **Active Policies** - Number of ActivityPolicies currently loaded

**Use case:** At-a-glance health check. Green stats = healthy system.

**Thresholds:**
- Error Rate: Green <1%, Yellow 1-5%, Red >5%

#### Row 2: Event Processing

Detailed event processing metrics:

- **Events by Type** - Audit events vs cluster events (stacked area chart)
- **Events Evaluated vs Generated** - Comparison of evaluation to generation (conversion efficiency)
- **Skipped Events by Reason** - Why events were skipped (stacked by skip reason)
- **Processing Duration p99 by Policy** - Latency distribution per policy

**Use case:** Troubleshooting processing issues. Identify which policies are slow, which events are being skipped, and conversion rates.

**What to look for:**
- High skip rate → policies may not be matching correctly
- Large gap between evaluated and generated → check policy rules
- High p99 latency → identify slow policies for optimization

#### Row 3: NATS Health

NATS connection and messaging metrics:

- **NATS Connection Status** - Connected (green) or Disconnected (red)
- **NATS Disconnects** - Total disconnection events (should be low)
- **NATS Publish Latency p99** - Message publish latency
- **Messages Published Rate** - Messages/sec published to NATS
- **NATS Connection Events** - Disconnects, reconnects, errors over time
- **NATS Publish Latency Percentiles** - p50, p95, p99 latency distribution

**Use case:** Diagnosing NATS connectivity issues. Monitor connection stability and publish performance.

**Thresholds:**
- Connection Status: Shows "Disconnected" if ANY instance is disconnected (use min())
- Disconnects: Green 0, Yellow 1-5, Red >5
- Publish Latency: Green <100ms, Yellow 100-500ms, Red >500ms

**What to look for:**
- Frequent disconnects → network instability or NATS server issues
- High publish latency → NATS backpressure or network congestion
- Connection status red → immediate issue requiring attention

#### Row 4: Worker Health

Worker goroutine and error metrics:

- **Active Workers** - Total worker goroutines across all instances
- **Error Types Breakdown** - Processing errors by type (stacked by error_type)

**Use case:** Understanding error patterns and worker concurrency.

**What to look for:**
- `cel_evaluation` errors → policy CEL expressions have bugs
- `json_unmarshal` errors → malformed events from upstream
- `nats_publish` errors → NATS publish issues
- Worker count stable → healthy concurrency

### Dashboard Variables

The dashboard includes a datasource template variable:

- **datasource** - Select Prometheus datasource (defaults to configured datasource)

Use this if multiple Prometheus instances are available.

### Time Range and Refresh

Default settings:
- **Time range:** Last 24 hours
- **Refresh:** Auto-refresh enabled (configurable in UI)

Adjust time range in Grafana UI to focus on specific time periods.

### Using the Dashboard

#### Investigating High Error Rate

1. Check **Error Rate** stat panel - if >5% proceed
2. View **Error Types Breakdown** - identify dominant error type
3. Check processor logs for specific error messages
4. If `cel_evaluation` errors, review policy CEL expressions
5. If `nats_publish` errors, check NATS health section

#### Investigating Slow Processing

1. Check **Processing Duration p99 by Policy** - identify slow policies
2. Review policy CEL complexity for slow policies
3. Check if slow policies handle large volumes (Events by Type)
4. Consider optimizing CEL expressions or splitting policies

#### Investigating Activity Generation Issues

1. Check **Active Policies** - should be >0
2. View **Events Evaluated vs Generated** - should track together
3. Check **Skipped Events by Reason** - high skip rate indicates matching issues
4. Verify policy match expressions are correct

#### Investigating NATS Issues

1. Check **NATS Connection Status** - should show "Connected"
2. View **NATS Disconnects** - frequent disconnects indicate instability
3. Check **NATS Publish Latency p99** - high latency indicates backpressure
4. Review **NATS Connection Events** timeline for patterns

### Dashboard Panels Reference

Complete list of panels and queries:

| Panel | Query | Purpose |
|-------|-------|---------|
| Event Processing Rate | `sum(rate(activity_processor_events_received_total[5m]))` | Incoming event volume (all sources) |
| Activity Generation Rate | `sum(rate(activity_processor_activities_generated_total[5m]))` | Output activity volume |
| Error Rate | `sum(rate(activity_processor_events_errored_total[5m])) / sum(rate(activity_processor_events_received_total[5m])) * 100` | Processing health |
| Active Policies | `activity_processor_active_policies` | Policy cache state |
| Events by API Group | `sum(rate(activity_processor_events_received_total[5m])) by (api_group)` | Event breakdown by API group |
| Events Evaluated vs Generated | `sum(rate(activity_processor_events_evaluated_total[5m]))` | Conversion efficiency |
|  | `sum(rate(activity_processor_activities_generated_total[5m]))` |  |
| Skipped Events by Reason | `sum(rate(activity_processor_events_skipped_total[5m])) by (reason)` | Skip reason breakdown |
| Processing Duration p99 by Policy | `histogram_quantile(0.99, sum(rate(activity_processor_event_processing_duration_seconds_bucket[5m])) by (policy, le))` | Policy performance |
| NATS Connection Status | `min(activity_processor_nats_connection_status)` | Connection health |
| NATS Disconnects | `sum(activity_processor_nats_disconnects_total)` | Disconnect count |
| NATS Publish Latency p99 | `histogram_quantile(0.99, sum(rate(activity_processor_nats_publish_latency_seconds_bucket[5m])) by (le))` | Publish performance |
| Messages Published Rate | `sum(rate(activity_processor_nats_messages_published_total[5m]))` | NATS throughput |
| Active Workers | `sum(activity_processor_active_workers)` | Concurrency level |
| Error Types Breakdown | `sum(rate(activity_processor_events_errored_total[5m])) by (error_type)` | Error classification |

## Deployment Configuration

Health probes and metrics are configured via environment variables in deployment manifests.

### activity-processor

```yaml
env:
  - name: HEALTH_PROBE_ADDR
    value: ":8081"
```

Metrics exposed on same port at `/metrics` endpoint.

### k8s-event-exporter

```yaml
env:
  - name: HEALTH_PROBE_ADDR
    value: ":8081"
```

### activity-controller-manager

```yaml
env:
  - name: METRICS_ADDR
    value: ":8080"
  - name: HEALTH_PROBE_ADDR
    value: ":8081"
```

Controller exposes metrics on separate port (8080) from health (8081).

## ServiceMonitor Configuration

Prometheus scrapes metrics via ServiceMonitor resources (if using Prometheus Operator).

Example ServiceMonitor for activity-processor:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: activity-processor
  namespace: activity-system
spec:
  selector:
    matchLabels:
      app: activity-processor
  endpoints:
    - port: health
      path: /metrics
      interval: 30s
```

Verify ServiceMonitor is discovered:

```bash
kubectl get servicemonitor -n activity-system
```

Check Prometheus targets to verify scraping:

```bash
# Port-forward to Prometheus
kubectl port-forward -n monitoring svc/prometheus 9090:9090

# Navigate to http://localhost:9090/targets
# Look for activity-processor, k8s-event-exporter, activity-controller-manager
```

## Troubleshooting Observability

### Metrics Not Appearing in Prometheus

1. Check ServiceMonitor exists and matches service labels
2. Verify Prometheus has permissions to scrape namespace
3. Check Prometheus targets page for scrape errors
4. Test metrics endpoint directly:

```bash
kubectl exec -n activity-system <pod> -- wget -qO- http://localhost:8081/metrics
```

### Dashboard Shows No Data

1. Verify Prometheus datasource is configured in Grafana
2. Check time range - may be outside data retention
3. Verify queries are correct (test in Prometheus UI first)
4. Check if metrics exist in Prometheus:

```bash
# Port-forward to Prometheus
kubectl port-forward -n monitoring svc/prometheus 9090:9090

# Query specific metric
# Navigate to http://localhost:9090/graph
# Enter: activity_processor_events_received_total
```

### Alerts Not Firing

1. Check PrometheusRule is loaded:

```bash
kubectl get prometheusrules -n activity-system
kubectl describe prometheusrule activity-alerts -n activity-system
```

2. Verify Prometheus has scraped the rule:

```bash
# In Prometheus UI: http://localhost:9090/alerts
# Should see all alert rules listed
```

3. Test alert expression in Prometheus:

```promql
# Copy alert expression from PrometheusRule
# Test in Prometheus graph page
```

4. Check Alertmanager configuration:

```bash
kubectl get pods -n monitoring -l app=alertmanager
kubectl logs -n monitoring -l app=alertmanager
```

### Health Probes Failing

1. Check probe configuration matches what component exposes
2. Test endpoint directly:

```bash
kubectl exec -n activity-system <pod> -- wget -qO- http://localhost:8081/healthz
kubectl exec -n activity-system <pod> -- wget -qO- http://localhost:8081/readyz
```

3. Check component logs for health check errors:

```bash
kubectl logs -n activity-system <pod> | grep -i health
```

4. Verify port matches probe configuration:

```bash
kubectl get pod -n activity-system <pod> -o jsonpath='{.spec.containers[*].ports}'
```

## Dashboard Navigation

The Activity system dashboards are designed for hierarchical investigation: start with the overview for health checks, then drill into specific component dashboards for detailed troubleshooting.

### Navigation Flow

```
Activity System Overview (health check)
├─> Events Pipeline (event collection issues)
├─> Activity Processor (activity generation issues)
└─> Audit Log Pipeline (audit log issues)
```

### When to Use Each Dashboard

**Start with Activity System Overview when:**
- Beginning incident triage
- Performing routine health checks
- Unclear which component is affected

**Navigate to Events Pipeline when:**
- Events not appearing in ClickHouse k8s_events table
- Event collection rate is zero or low
- Event exporter connection issues
- Investigating specific namespace event patterns

**Navigate to Activity Processor when:**
- Activities not being generated from audit events
- Policy evaluation errors occurring
- Audit event processing rate issues
- CEL expression evaluation problems

**Navigate to Audit Log Pipeline when:**
- Audit logs not flowing through Vector
- NATS stream backlog for audit events
- Vector routing or transformation issues
- ClickHouse write path problems for audit logs

### Quick Navigation Links

The Activity System Overview dashboard includes direct links to all detailed dashboards in the "Quick Links" section. Use these for fast navigation during incidents.

## Best Practices

### Monitoring

- **Review dashboard regularly** - Weekly review to spot trends
- **Set up Alertmanager routing** - Route critical alerts to paging, warnings to tickets
- **Test alerts in staging** - Verify alerts fire before production
- **Tune alert thresholds** - Adjust based on observed baselines to prevent fatigue
- **Start with overview dashboard** - Use hierarchical navigation from overview to detailed dashboards

### Metrics

- **Avoid high cardinality labels** - Don't use unbounded values (user IDs, timestamps)
- **Keep histograms focused** - Use appropriate buckets for your latency distribution
- **Clean up stale series** - Delete metrics for removed policies to prevent cardinality growth

### Health Probes

- **Liveness should rarely fail** - Set threshold higher than readiness
- **Readiness for temporary issues** - Use for cache sync, NATS connection
- **Fast probe responses** - Keep check logic simple, <1s response time
- **Test failure scenarios** - Verify probes correctly detect failures

### Dashboards

- **Keep panels focused** - One metric per panel for clarity
- **Use consistent time ranges** - Match panel queries to dashboard time range
- **Include descriptions** - Panel descriptions help operators understand metrics
- **Link to runbooks** - Add links to alert response procedures

## Common Troubleshooting Scenarios

### Events Not Appearing in ClickHouse

**Dashboard:** Events Pipeline

**Steps:**
1. Open Events Pipeline dashboard
2. Check **Exporter Connection Status** and **Informer Sync Status** - must be green
3. Verify **Events Published Rate** >0 (if cluster has activity)
4. Review **Event Flow Through Stages** - identify where events stop flowing
5. If published but not consumed, check NATS stream health
6. If consumed but not written, check **ClickHouse Insert Latency** and Vector logs

**Common causes:**
- Event exporter disconnected from NATS
- Informer cache not synced (pod restart)
- ClickHouse performance issues (check insert latency)
- Vector pipeline errors (check Vector logs)

### Activities Not Being Generated

**Dashboard:** Activity Processor

**Steps:**
1. Open Activity Processor dashboard
2. Check **Active Policies** - must be >0
3. View **Events Evaluated vs Generated** - should track together
4. Review **Skipped Events by Reason** - identify why events aren't matching
5. Check **Processing Duration p99 by Policy** - identify slow policies
6. Review processor logs for CEL evaluation errors

**Common causes:**
- No active ActivityPolicies loaded
- Policy match expressions not matching events
- CEL evaluation errors in policy rules
- NATS connection issues preventing event consumption

### Pipeline Backlog Growing

**Dashboard:** Activity System Overview → Events Pipeline or Audit Log Pipeline

**Steps:**
1. Open Activity System Overview dashboard
2. Check **Total Storage Rate** - if low, ClickHouse is bottleneck
3. Navigate to specific pipeline dashboard (Events or Audit)
4. Review **Queue Backlog** and consumer lag metrics
5. Check ClickHouse insert latency and performance
6. Verify Vector buffer depth - high values indicate slow writes

**Common causes:**
- ClickHouse performance degradation (CPU/memory/disk)
- ClickHouse cluster overloaded (check query concurrency)
- Network issues between Vector and ClickHouse
- Vector configuration issues (batch size, flush interval)

## See Also

- [Alert Response Runbook](../runbooks/alert-response.md) - Detailed procedures for responding to alerts
- [Architecture Documentation](../architecture/overview.md) - System architecture context
- [Deployment Guide](deployment.md) - Production deployment configuration
- [Activity System Overview Dashboard](/d/activity-system-overview) - Single-pane health view
- [Events Pipeline Dashboard](/d/events-pipeline) - K8s events monitoring
- [Activity Processor Dashboard](/d/activity-processor) - Activity generation monitoring
