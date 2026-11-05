// Audit Log Pipeline Dashboard
// Generated using Grafonnet v11.4.0
// To build: jsonnet -J vendor dashboards/audit-pipeline.jsonnet > ../config/components/observability/dashboards/generated/audit-pipeline.json

local g = import 'grafonnet-v11.4.0/main.libsonnet';
local config = import '../config.libsonnet';

local dashboard = g.dashboard;
local panel = g.panel;
local stat = panel.stat;
local timeSeries = panel.timeSeries;
local text = panel.text;
local row = panel.row;
local prometheus = g.query.prometheus;
local util = g.util;

// Configuration
local datasource = config.dashboards.datasource.name;
local datasourceRegex = config.dashboards.datasource.regex;
local refresh = config.dashboards.refresh;

// Reusable Queries
local queries = {
  // API Server metrics
  apiserverEventsGenerated: 'sum(rate(apiserver_audit_event_total{job=~".*apiserver.*"}[5m]))',
  apiserverErrors: 'sum(rate(apiserver_audit_error_total{job=~".*apiserver.*"}[5m])) by (plugin) or vector(0)',
  apiserverRequestsRejected: 'sum(apiserver_audit_requests_rejected_total{job=~".*apiserver.*"})',
  apiserverAuditLevels: 'sum(rate(apiserver_audit_level_total{job=~".*apiserver.*"}[5m])) by (level)',

  // Vector Sidecar (ingress from API servers)
  // Note: In dev/test environments, events come from file source (apiserver_audit_logs)
  // In production, they come from webhook source. Use nats_jetstream to capture both.
  vectorSidecarNatsSent: 'sum(rate(vector_component_sent_events_total{component_id="nats_jetstream",namespace="activity-system",pod=~"vector-sidecar.*"}[5m]))',
  vectorSidecarErrors: 'sum(rate(vector_component_errors_total{namespace="activity-system",pod=~"vector-sidecar.*"}[5m])) by (component_id) or vector(0)',

  // Vector Aggregator (NATS consumer to ClickHouse) - using recording rules
  vectorAggregatorNatsReceived: 'activity:vector_throughput:5m',
  vectorAggregatorClickhouseSent: 'activity:vector_writes:5m',
  vectorAggregatorErrors: 'sum(rate(vector_component_errors_total{namespace="activity-system",pod=~"vector-aggregator.*"}[5m])) by (component_id) or vector(0)',
  // Buffer depth for all ClickHouse sinks (audit_events, activities, k8s_events)
  vectorAggregatorBufferDepth: 'sum(vector_buffer_size_events{component_id=~"clickhouse_.*",namespace="activity-system"})',
  vectorAggregatorClickhouseHttpErrors: 'sum(rate(vector_http_client_errors_total{component_id=~"clickhouse_.*",namespace="activity-system"}[5m])) or vector(0)',
  vectorAggregatorClickhouseComponentErrors: 'sum(rate(vector_component_errors_total{component_id=~"clickhouse_.*",namespace="activity-system"}[5m])) or vector(0)',

  // End-to-end latency metrics (custom metric from Vector transform)
  // This measures true end-to-end latency: K8s API event generation → Vector aggregator processing
  endToEndLatencyP99: 'histogram_quantile(0.99, sum(rate(activity_pipeline_end_to_end_latency_seconds_bucket{stage="nats_to_aggregator"}[5m])) by (le))',
  endToEndLatencyP95: 'histogram_quantile(0.95, sum(rate(activity_pipeline_end_to_end_latency_seconds_bucket{stage="nats_to_aggregator"}[5m])) by (le))',
  endToEndLatencyP50: 'histogram_quantile(0.50, sum(rate(activity_pipeline_end_to_end_latency_seconds_bucket{stage="nats_to_aggregator"}[5m])) by (le))',

  // NATS metrics - using recording rules where available
  natsQueuePending: 'sum(activity:nats_consumer_lag)',
  natsQueueAckPending: 'sum(nats_consumer_num_ack_pending{consumer_name="clickhouse-ingest"})',

  // ClickHouse metrics
  //
  // We use a replicated database so we need to factor in that multiple replicas
  // will contain the same data.
  clickhouseInsertedRows: 'avg(rate(chi_clickhouse_table_parts_rows{chi="activity-clickhouse", database="audit", table="audit_logs", active="1"}[5m]))',
  clickhouseMergedRows: 'avg(rate(chi_clickhouse_event_MergedRows{chi="activity-clickhouse"}[5m]))',
  // Note: chi_clickhouse_metric_Query shows all active queries (no insert-specific gauge exists)
  clickhouseActiveInserts: 'sum(chi_clickhouse_metric_Query{chi="activity-clickhouse"}) or vector(0)',
  clickhouseActiveQueries: 'sum(chi_clickhouse_metric_Query{chi="activity-clickhouse"}) or vector(0)',
  clickhouseActiveMerges: 'sum(chi_clickhouse_metric_Merge{chi="activity-clickhouse"}) or vector(0)',
  clickhouseTableParts: 'avg(chi_clickhouse_table_parts{chi="activity-clickhouse",database="audit",table="audit_logs"})',
  clickhouseInsertLatency: 'sum(rate(chi_clickhouse_event_InsertQueryTimeMicroseconds{chi="activity-clickhouse"}[5m])) / clamp_min(sum(rate(chi_clickhouse_event_InsertQuery{chi="activity-clickhouse"}[5m])), 0.001) / 1000000',

  // Activity API metrics
  // Note: Using apiserver request duration (recording rules exist for these)
  activityQueryLatencyP50: 'activity:apiserver_request_duration:p50',
  activityQueryLatencyP95: 'activity:apiserver_request_duration:p95',
  activityQueryLatencyP99: 'activity:apiserver_request_duration:p99',
  // Note: activity_clickhouse_query_total not yet instrumented - using request rate as proxy
  activityQuerySuccess: 'sum(rate(apiserver_request_total{job="activity-apiserver",code=~"2.."}[5m])) or vector(0)',
  activityQueryErrors: 'sum(rate(apiserver_request_total{job="activity-apiserver",code=~"5.."}[5m])) or vector(0)',
  activityQueryErrorsByType: 'sum(rate(apiserver_request_total{job="activity-apiserver",code=~"[45].."}[5m])) by (code) or vector(0)',
  // Note: activity_cel_filter_errors_total not instrumented - CEL parse duration exists but not errors
  activityCelFilterErrors: 'vector(0)',
  // Note: activity_auditlog_queries_by_scope_total not yet instrumented
  activityQueriesByScope: 'sum(rate(apiserver_request_total{job="activity-apiserver",resource=~"auditlogqueries|activityqueries|eventqueries"}[5m])) by (resource) or vector(0)',

  // Combined metrics
  pipelineErrorRate: '(sum(rate(vector_component_errors_total{namespace="activity-system"}[5m])) or vector(0)) + (sum(rate(activity_clickhouse_query_errors_total[5m])) or vector(0))',
  pipelineIngressRate: 'sum(rate(vector_component_sent_events_total{component_id="nats_jetstream",namespace="activity-system",pod=~"vector-sidecar.*"}[5m]))',
  // Component health: Check monitored Activity services (apiserver, processor, k8s-event-exporter)
  // Note: Vector and NATS don't have ServiceMonitors configured, so we check their health
  // via functional metrics (event throughput) instead of 'up' metrics
  componentHealth: 'min(up{namespace="activity-system",pod=~"activity-.*|k8s-event-exporter-.*"})',
};

// Build all panels
local allPanels =
  // ============================================================================
  // Row 1: Critical Health Indicators
  // ============================================================================
  [
    row.new('Critical Health Indicators')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(0),
  ]
  + util.grid.makeGrid([
    stat.new('Ingress Rate')
    + stat.panelOptions.withDescription('Events/sec entering pipeline from K8s API servers')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('value')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('ops')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.pipelineIngressRate)
      + prometheus.withLegendFormat('Events/sec'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'red', value: null },
      { color: 'yellow', value: 1 },
      { color: 'green', value: 10 },
    ]),

    stat.new('Egress Rate')
    + stat.panelOptions.withDescription('Events/sec written to ClickHouse (should match ingress)')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('value')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('ops')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorAggregatorClickhouseSent)
      + prometheus.withLegendFormat('Events/sec'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'red', value: null },
      { color: 'yellow', value: 1 },
      { color: 'green', value: 10 },
    ]),

    stat.new('Queue Backlog')
    + stat.panelOptions.withDescription('Messages pending in NATS queue (indicator of backpressure)')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('short')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.natsQueuePending)
      + prometheus.withLegendFormat('Pending'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'yellow', value: 1000 },
      { color: 'red', value: 10000 },
    ]),

    stat.new('Error Rate')
    + stat.panelOptions.withDescription('Combined errors across all pipeline components')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('ops')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.pipelineErrorRate)
      + prometheus.withLegendFormat('Errors/sec'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'yellow', value: 0.1 },
      { color: 'red', value: 1 },
    ]),

    stat.new('ClickHouse Insert Latency')
    + stat.panelOptions.withDescription('Average time to write events to ClickHouse')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.standardOptions.withUnit('s')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseInsertLatency)
      + prometheus.withLegendFormat('Insert Latency'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'yellow', value: 0.1 },
      { color: 'orange', value: 0.5 },
      { color: 'red', value: 1.0 },
    ]),

    stat.new('Component Health')
    + stat.panelOptions.withDescription('Activity components UP (apiserver, processor, event-exporter). Vector/NATS health monitored via functional metrics.')
    + stat.options.withTextMode('value_and_name')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.componentHealth)
      + prometheus.withLegendFormat('Status'),
    ])
    + stat.standardOptions.withMappings([
      { type: 'value', options: { '0': { text: 'DOWN', color: 'red' } } },
      { type: 'value', options: { '1': { text: 'ALL UP', color: 'green' } } },
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'red', value: null },
      { color: 'green', value: 1 },
    ]),

    stat.new('Requests Rejected')
    + stat.panelOptions.withDescription('API requests rejected due to audit backend failure - CRITICAL if >0')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('short')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.apiserverRequestsRejected)
      + prometheus.withLegendFormat('Rejected'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'red', value: 1 },
    ]),
  ], panelWidth=3, panelHeight=4, startY=1)

  // ============================================================================
  // Row 2: API Server Audit Generation
  // ============================================================================
  + [
    row.new('API Server Audit Generation')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(5),
  ]
  + util.grid.makeGrid([
    timeSeries.new('Audit Event Generation vs Ingestion')
    + timeSeries.panelOptions.withDescription('API server event generation vs Vector ingestion - gap indicates lost events')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(0)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.apiserverEventsGenerated)
      + prometheus.withLegendFormat('API Server Generated'),
      prometheus.new('$datasource', queries.vectorSidecarNatsSent)
      + prometheus.withLegendFormat('Vector Sidecar → NATS'),
    ]),

    timeSeries.new('Audit Backend Errors')
    + timeSeries.panelOptions.withDescription('Audit events that failed to be written - should be ZERO in healthy state')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.apiserverErrors)
      + prometheus.withLegendFormat('{{plugin}} errors'),
    ]),

    timeSeries.new('Audit Policy Levels')
    + timeSeries.panelOptions.withDescription('Distribution of audit events by policy level (Metadata/Request/RequestResponse)')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.apiserverAuditLevels)
      + prometheus.withLegendFormat('{{level}}'),
    ]),
  ], panelWidth=8, panelHeight=7, startY=6)

  // ============================================================================
  // Row 3: Pipeline Flow Visualization
  // ============================================================================
  + [
    row.new('Pipeline Flow')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(13),
  ]
  + util.grid.makeGrid([
    timeSeries.new('Event Flow Through Pipeline Stages')
    + timeSeries.panelOptions.withDescription('Events per second at each pipeline stage - should be roughly equal in steady state')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorSidecarNatsSent)
      + prometheus.withLegendFormat('1. Sidecar → NATS'),
      prometheus.new('$datasource', queries.vectorAggregatorNatsReceived)
      + prometheus.withLegendFormat('2. Aggregator ← NATS'),
      prometheus.new('$datasource', queries.vectorAggregatorClickhouseSent)
      + prometheus.withLegendFormat('3. Aggregator → ClickHouse'),
      prometheus.new('$datasource', queries.clickhouseInsertedRows)
      + prometheus.withLegendFormat('4. ClickHouse Inserts'),
    ]),

    timeSeries.new('Ingress vs Egress Comparison')
    + timeSeries.panelOptions.withDescription('Pipeline input vs output - gap indicates bottleneck or filtering')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(0)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorSidecarNatsSent)
      + prometheus.withLegendFormat('Ingress (Sidecar → NATS)'),
      prometheus.new('$datasource', queries.clickhouseInsertedRows)
      + prometheus.withLegendFormat('Egress (ClickHouse Writes)'),
    ]),
  ], panelWidth=12, panelHeight=8, startY=14)

  // ============================================================================
  // Row 4: Performance Deep Dive
  // ============================================================================
  + [
    row.new('Performance Deep Dive')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(22),
  ]
  + util.grid.makeGrid([
    timeSeries.new('End-to-End Latency (p50/p95/p99)')
    + timeSeries.panelOptions.withDescription('Time from K8s API event generation to Vector aggregator processing (stageTimestamp → now)')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('s')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.endToEndLatencyP99)
      + prometheus.withLegendFormat('p99'),
      prometheus.new('$datasource', queries.endToEndLatencyP95)
      + prometheus.withLegendFormat('p95'),
      prometheus.new('$datasource', queries.endToEndLatencyP50)
      + prometheus.withLegendFormat('p50'),
    ]),

    timeSeries.new('NATS Queue Backlog')
    + timeSeries.panelOptions.withDescription('Pending and unacknowledged messages - indicators of backpressure')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('short')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(20)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.natsQueuePending)
      + prometheus.withLegendFormat('Pending Messages'),
      prometheus.new('$datasource', queries.natsQueueAckPending)
      + prometheus.withLegendFormat('Unacknowledged'),
    ]),

    timeSeries.new('Vector Aggregator Buffer Depth')
    + timeSeries.panelOptions.withDescription('Buffer depth indicates ClickHouse backpressure - high values mean slow writes')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('short')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorAggregatorBufferDepth)
      + prometheus.withLegendFormat('Buffered Events'),
    ]),

    timeSeries.new('ClickHouse Insert Performance')
    + timeSeries.panelOptions.withDescription('Insert rate and latency - write path health')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseInsertedRows)
      + prometheus.withLegendFormat('Rows Inserted/sec'),
      prometheus.new('$datasource', queries.clickhouseActiveInserts)
      + prometheus.withLegendFormat('Active Insert Queries'),
    ]),
  ], panelWidth=12, panelHeight=7, startY=23)

  // ============================================================================
  // Row 5: Resource Saturation
  // ============================================================================
  + [
    row.new('Resource Saturation')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(37),
  ]
  + util.grid.makeGrid([
    timeSeries.new('ClickHouse Active Operations')
    + timeSeries.panelOptions.withDescription('Concurrent operations - high values indicate database saturation')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('short')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(1)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseActiveInserts)
      + prometheus.withLegendFormat('Inserts'),
      prometheus.new('$datasource', queries.clickhouseActiveQueries)
      + prometheus.withLegendFormat('Queries'),
      prometheus.new('$datasource', queries.clickhouseActiveMerges)
      + prometheus.withLegendFormat('Merges'),
    ]),

    timeSeries.new('Merge Activity vs Insert Rate')
    + timeSeries.panelOptions.withDescription('Merge rate should be 10-100x insert rate for healthy consolidation')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseInsertedRows)
      + prometheus.withLegendFormat('Insert Rate'),
      prometheus.new('$datasource', queries.clickhouseMergedRows)
      + prometheus.withLegendFormat('Merge Rate'),
    ]),

    timeSeries.new('Part Count (Query Performance Indicator)')
    + timeSeries.panelOptions.withDescription('Number of data parts - high count (>100) slows queries, needs more merging')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('short')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseTableParts)
      + prometheus.withLegendFormat('Total Parts'),
    ]),
  ], panelWidth=8, panelHeight=7, startY=38)

  // ============================================================================
  // Row 6: Error Breakdown (collapsed)
  // ============================================================================
  + [
    row.new('Error Breakdown')
    + row.withCollapsed(true)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(45)
    + row.withPanels(
      util.grid.makeGrid([
    timeSeries.new('Errors by Component')
    + timeSeries.panelOptions.withDescription('Breakdown of errors across all pipeline components')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('right')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(1)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorSidecarErrors)
      + prometheus.withLegendFormat('Sidecar: {{component_id}}'),
      prometheus.new('$datasource', queries.vectorAggregatorErrors)
      + prometheus.withLegendFormat('Aggregator: {{component_id}}'),
      prometheus.new('$datasource', queries.activityQueryErrorsByType)
      + prometheus.withLegendFormat('Activity: {{error_type}}'),
      prometheus.new('$datasource', queries.activityCelFilterErrors)
      + prometheus.withLegendFormat('CEL: {{error_type}}'),
    ]),

    timeSeries.new('Vector Aggregator Errors Detail')
    + timeSeries.panelOptions.withDescription('Vector aggregator error breakdown - most common failure point')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorAggregatorClickhouseHttpErrors)
      + prometheus.withLegendFormat('HTTP Client Errors'),
      prometheus.new('$datasource', queries.vectorAggregatorClickhouseComponentErrors)
      + prometheus.withLegendFormat('Component Errors'),
    ]),
  ], panelWidth=12, panelHeight=8, startY=46)
    ),
  ]

  // ============================================================================
  // Row 7: Query Performance (collapsed)
  // ============================================================================
  + [
    row.new('Query Performance')
    + row.withCollapsed(true)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(46)
    + row.withPanels(
      util.grid.makeGrid([
    timeSeries.new('Activity API Query Latency')
    + timeSeries.panelOptions.withDescription('User-facing query performance (p50/p95/p99)')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull', 'max'])
    + timeSeries.standardOptions.withUnit('s')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.activityQueryLatencyP50)
      + prometheus.withLegendFormat('p50'),
      prometheus.new('$datasource', queries.activityQueryLatencyP95)
      + prometheus.withLegendFormat('p95'),
      prometheus.new('$datasource', queries.activityQueryLatencyP99)
      + prometheus.withLegendFormat('p99'),
    ]),

    timeSeries.new('Query Rate & Errors')
    + timeSeries.panelOptions.withDescription('Query volume and error rates')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.activityQuerySuccess)
      + prometheus.withLegendFormat('Successful Queries'),
      prometheus.new('$datasource', queries.activityQueryErrors)
      + prometheus.withLegendFormat('Failed Queries'),
    ]),

    timeSeries.new('Query Patterns')
    + timeSeries.panelOptions.withDescription('Query distribution by scope type')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
    + timeSeries.options.legend.withShowLegend(true)
    + timeSeries.options.legend.withCalcs(['mean', 'lastNotNull'])
    + timeSeries.standardOptions.withUnit('ops')
    + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
    + timeSeries.fieldConfig.defaults.custom.withLineWidth(2)
    + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
    + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
    + timeSeries.datasource.withType('prometheus')
    + timeSeries.datasource.withUid('$datasource')
    + timeSeries.queryOptions.withTargets([
      prometheus.new('$datasource', queries.activityQueriesByScope)
      + prometheus.withLegendFormat('{{scope_type}}'),
    ]),
  ], panelWidth=8, panelHeight=7, startY=47)
    ),
  ];

// Dashboard
dashboard.new('Audit Log Pipeline')
+ dashboard.withDescription('Concise, single-screen overview for rapid issue identification. End-to-end monitoring: K8s API → Vector → NATS → ClickHouse → Activity API. For K8s Events pipeline, see Events Pipeline dashboard.')
+ dashboard.withTags(['audit', 'pipeline', 'activity', 'observability'])
+ dashboard.withUid('audit-pipeline')
+ dashboard.time.withFrom(config.dashboards.timeRange.from)
+ dashboard.time.withTo(config.dashboards.timeRange.to)
+ dashboard.withTimezone(config.dashboards.timezone)
+ dashboard.withRefresh(refresh)
+ dashboard.withEditable(true)
+ dashboard.graphTooltip.withSharedCrosshair()
+ dashboard.withVariables([
  g.dashboard.variable.datasource.new('datasource', config.dashboards.datasource.type)
  + g.dashboard.variable.datasource.generalOptions.withLabel('Prometheus Datasource')
  + g.dashboard.variable.datasource.withRegex(datasourceRegex),
])
+ dashboard.withPanels(allPanels)
