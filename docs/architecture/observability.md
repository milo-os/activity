# Observability

The activity service exports metrics, traces, and logs to help operators
understand system performance and troubleshoot issues.

## Metrics

All components export Prometheus metrics for monitoring and alerting.

### Activity API Server

The API server exports metrics at the `/metrics` endpoint (port 8080):

| Metric | Type | Description |
|--------|------|-------------|
| `activity_clickhouse_query_duration_seconds` | Histogram | ClickHouse query latency |
| `activity_clickhouse_query_total` | Counter | Total queries by status |
| `activity_clickhouse_query_errors_total` | Counter | Failed queries by error type |
| `activity_auditlog_query_results_total` | Histogram | Results returned per query |
| `activity_cel_filter_parse_duration_seconds` | Histogram | CEL filter compilation time |
| `activity_cel_filter_errors_total` | Counter | CEL compilation errors by type |
| `activity_auditlog_queries_by_scope_total` | Counter | Queries by scope type |
| `activity_auditlog_query_lookback_duration_seconds` | Histogram | How far back queries look |
| `activity_auditlog_query_time_range_seconds` | Histogram | Query time range duration |

### Vector Pipeline

The Vector aggregator exports pipeline metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `activity_pipeline_end_to_end_latency_seconds` | Histogram | Latency from event generation to aggregator |
| `vector_component_received_events_total` | Counter | Events received per component |
| `vector_component_sent_events_total` | Counter | Events sent per component |
| `vector_buffer_size_events` | Gauge | Events in buffer |
| `vector_buffer_size_bytes` | Gauge | Buffer size in bytes |

### NATS JetStream

NATS exports consumer and stream metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `jetstream_consumer_num_pending` | Gauge | Pending messages per consumer |
| `jetstream_consumer_num_ack_pending` | Gauge | Unacknowledged messages |
| `jetstream_stream_messages` | Gauge | Total messages in stream |
| `jetstream_stream_bytes` | Gauge | Stream size in bytes |

### ClickHouse

ClickHouse system tables provide query and insert metrics:

| Table | Purpose |
|-------|---------|
| `system.query_log` | Query execution history |
| `system.metrics` | Current metric values |
| `system.asynchronous_metrics` | Background metrics |
| `system.events` | Cumulative event counters |

## Tracing

The API server supports distributed tracing via OpenTelemetry.

### Configuration

Traces export to an OTLP-compatible backend (Tempo, Jaeger, etc.):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: activity-apiserver-config
data:
  tracing.yaml: |
    exporters:
      otlp:
        endpoint: tempo.observability:4317
        insecure: true
    sampling:
      ratePerMillion: 10000  # 1% in production
```

### Sampling Rates

| Environment | Sampling Rate | Configuration |
|-------------|---------------|---------------|
| Development | 100% | `samplingRatePerMillion: 1000000` |
| Staging | 10% | `samplingRatePerMillion: 100000` |
| Production | 1% | `samplingRatePerMillion: 10000` |

### Traced Operations

Traces include spans for:

| Span | Description |
|------|-------------|
| `http.request` | Full HTTP request handling |
| `clickhouse.query` | ClickHouse query execution |
| `cel.compile` | CEL filter compilation |
| `cel.evaluate` | CEL expression evaluation |
| `nats.publish` | NATS message publishing |
| `nats.consume` | NATS message consumption |

### Trace Attributes

Common attributes on all spans:

| Attribute | Description |
|-----------|-------------|
| `scope.type` | Tenant scope type |
| `scope.name` | Tenant scope name |
| `user.uid` | Authenticated user UID |
| `query.time_range` | Query time range duration |

## Dashboards

Two Grafana dashboards are provided for monitoring.

### Activity API Server Dashboard

Focuses on query performance and behavior:

**Panels:**
- API request rate by endpoint
- Query latency percentiles (p50, p90, p99)
- Error rate by type
- ClickHouse query duration correlation
- CEL filter compilation time
- Results per query distribution
- Queries by scope type

**Use Cases:**
- Diagnose slow queries reported by users
- Identify CEL filter patterns causing issues
- Monitor scope distribution for capacity planning

### Audit Pipeline Dashboard

Monitors the ingestion pipeline from Vector sidecar through NATS to ClickHouse:

**Panels:**
- Event throughput (events/sec)
- End-to-end latency percentiles
- NATS consumer lag
- Buffer utilization (sidecar and aggregator)
- Delivery success rate
- ClickHouse insert rate and batch size
- Deduplication rate

**Use Cases:**
- Identify ingestion bottlenecks
- Monitor NATS consumer backlog
- Detect delivery failures
- Capacity planning for ClickHouse

## Alerting

Recommended alerts for production deployments:

### Critical Alerts

| Alert | Condition | Description |
|-------|-----------|-------------|
| `ActivityAPIServerDown` | `up{job="activity-apiserver"} == 0` | API server not responding |
| `ClickHouseDown` | `up{job="clickhouse"} == 0` | ClickHouse not responding |
| `NATSDown` | `up{job="nats"} == 0` | NATS not responding |

### Warning Alerts

| Alert | Condition | Description |
|-------|-----------|-------------|
| `HighQueryLatency` | `histogram_quantile(0.99, ...) > 5` | P99 query latency > 5s |
| `NATSConsumerLag` | `jetstream_consumer_num_pending > 100000` | Consumer falling behind |
| `BufferNearFull` | `vector_buffer_size_events / vector_buffer_max_size_events > 0.8` | Buffer 80% full |
| `HighErrorRate` | `rate(errors[5m]) / rate(requests[5m]) > 0.01` | Error rate > 1% |

## Logging

All components use structured JSON logging:

```json
{
  "level": "info",
  "ts": "2024-01-15T10:30:00.000Z",
  "logger": "activity-apiserver",
  "msg": "query executed",
  "scope_type": "project",
  "scope_name": "prod",
  "duration_ms": 42,
  "result_count": 100
}
```

### Log Levels

| Level | Purpose |
|-------|---------|
| `error` | Failures requiring attention |
| `warn` | Degraded performance or unusual conditions |
| `info` | Normal operational events |
| `debug` | Detailed debugging (disabled in production) |

## Related Documentation

- [Architecture Overview](./README.md)
- [Audit Pipeline](./audit-pipeline.md)
