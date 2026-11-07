// Control Plane Events Pipeline Dashboard
// Generated using Grafonnet v11.4.0
// To build: jsonnet -J vendor dashboards/events-pipeline.jsonnet > ../config/components/observability/dashboards/generated/events-pipeline.json

local g = import 'grafonnet-v11.4.0/main.libsonnet';
local config = import '../config.libsonnet';

local dashboard = g.dashboard;
local panel = g.panel;
local stat = panel.stat;
local timeSeries = panel.timeSeries;
local row = panel.row;
local prometheus = g.query.prometheus;
local util = g.util;

// Configuration
local datasource = config.dashboards.datasource.name;
local datasourceRegex = config.dashboards.datasource.regex;
local refresh = config.dashboards.refresh;

// Reusable Queries
local queries = {
  // Pipeline flow: [Activity API Server OR k8s-event-exporter] → NATS → Vector → ClickHouse
  //
  // Metrics support both production (apiserver) and dev (k8s-event-exporter) environments.
  // Queries use `or` to combine both sources - whichever is running provides the data.

  // Event ingestion rate (from either apiserver or k8s-event-exporter)
  eventsPublishedRate: '(sum(rate(activity_events_published_total[5m])) or vector(0)) + (sum(rate(event_exporter_events_published_total[5m])) or vector(0))',
  eventsPublishedByNamespace: 'sum(rate(activity_events_published_total[5m])) by (namespace) or sum(rate(event_exporter_events_published_total[5m])) by (namespace)',
  eventsPublishedByReason: 'sum(rate(activity_events_published_total[5m])) by (reason) or sum(rate(event_exporter_events_published_total[5m])) by (reason)',

  // Publish latency (from either apiserver or k8s-event-exporter)
  publishLatencyP99: 'histogram_quantile(0.99, sum(rate(activity_events_publish_latency_seconds_bucket[5m])) by (le)) or histogram_quantile(0.99, sum(rate(event_exporter_publish_latency_seconds_bucket[5m])) by (le))',
  publishLatencyP95: 'histogram_quantile(0.95, sum(rate(activity_events_publish_latency_seconds_bucket[5m])) by (le)) or histogram_quantile(0.95, sum(rate(event_exporter_publish_latency_seconds_bucket[5m])) by (le))',
  publishLatencyP50: 'histogram_quantile(0.50, sum(rate(activity_events_publish_latency_seconds_bucket[5m])) by (le)) or histogram_quantile(0.50, sum(rate(event_exporter_publish_latency_seconds_bucket[5m])) by (le))',

  // Ingestion errors (from either source)
  publishErrors: '(sum(rate(activity_events_publish_errors_total[5m])) or vector(0)) + (sum(rate(event_exporter_publish_errors_total[5m])) or vector(0))',

  // Ingestion source health (shows status from whichever source is active)
  // In dev: k8s-event-exporter NATS connection status
  // In prod: activity-apiserver events NATS connection status
  ingestionSourceStatus: 'min(activity_events_nats_connection_status) or min(event_exporter_nats_connection_status{job="k8s-event-exporter"})',
  exporterConnectionStatus: 'min(event_exporter_nats_connection_status{job="k8s-event-exporter"})',
  informerSyncStatus: 'min(event_exporter_informer_synced{job="k8s-event-exporter"})',

  // NATS metrics (events stream) - same in all environments
  natsEventsStreamMessages: 'sum(rate(nats_stream_total_messages{stream_name="EVENTS"}[5m]))',
  natsEventsQueuePending: 'sum(nats_consumer_num_pending{consumer_name="clickhouse-ingest-events"}) or vector(0)',
  natsEventsQueueAckPending: 'sum(nats_consumer_num_ack_pending{consumer_name="clickhouse-ingest-events"}) or vector(0)',

  // Vector metrics (events consumer → ClickHouse) - same in all environments
  vectorNatsEventsReceived: 'sum(rate(vector_component_received_events_total{component_id="nats_events_consumer",namespace="activity-system"}[5m]))',
  vectorClickhouseEventsSent: 'sum(rate(vector_component_sent_events_total{component_id="clickhouse_k8s_events",namespace="activity-system"}[5m]))',
  vectorEventsErrors: 'sum(rate(vector_component_errors_total{component_id="clickhouse_k8s_events",namespace="activity-system"}[5m])) or vector(0)',
  vectorEventsBufferDepth: 'sum(vector_buffer_size_events{component_id="clickhouse_k8s_events",namespace="activity-system"}) or vector(0)',

  // ClickHouse metrics (k8s_events table) - same in all environments
  clickhouseEventsWriteRate: 'activity:clickhouse_events_insert_rate:5m',
  clickhouseEventsInsertLatency: 'activity:clickhouse_events_insert_latency',
  clickhouseEventsTableRows: 'sum(chi_clickhouse_table_parts_rows{chi="activity-clickhouse",database="audit",table="k8s_events",active="1"})',
  clickhouseEventsTableParts: 'avg(chi_clickhouse_table_parts{chi="activity-clickhouse",database="audit",table="k8s_events"})',

  // Activity API Server - EventQuery metrics (read path)
  eventQueryRate: 'sum(rate(apiserver_request_total{job="activity-apiserver",resource="eventqueries"}[5m])) or vector(0)',
  // WATCH requests excluded: long-lived connections skew latency percentiles.
  eventQueryLatencyP99: 'histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{job="activity-apiserver",resource="eventqueries",verb!="WATCH"}[5m])) by (le))',
  eventQueryErrors: 'sum(rate(apiserver_request_total{job="activity-apiserver",resource="eventqueries",code=~"5.."}[5m])) or vector(0)',

  // Combined pipeline health
  pipelineErrorRate: '(sum(rate(activity_events_publish_errors_total[5m])) or vector(0)) + (sum(rate(event_exporter_publish_errors_total[5m])) or vector(0)) + (sum(rate(vector_component_errors_total{component_id="clickhouse_k8s_events",namespace="activity-system"}[5m])) or vector(0))',
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
    stat.new('Events Ingested')
    + stat.panelOptions.withDescription('Events/sec published to NATS (from apiserver or event-exporter)')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('value')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('ops')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.eventsPublishedRate)
      + prometheus.withLegendFormat('Events/sec'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'red', value: null },
      { color: 'yellow', value: 0.1 },
      { color: 'green', value: 1 },
    ]),

    stat.new('Events Written Rate')
    + stat.panelOptions.withDescription('Events/sec written to ClickHouse k8s_events table')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('value')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('ops')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseEventsWriteRate)
      + prometheus.withLegendFormat('Events/sec'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'red', value: null },
      { color: 'yellow', value: 0.1 },
      { color: 'green', value: 1 },
    ]),

    stat.new('Queue Backlog')
    + stat.panelOptions.withDescription('Pending events in NATS queue (backpressure indicator)')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('short')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.natsEventsQueuePending)
      + prometheus.withLegendFormat('Pending'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'yellow', value: 100 },
      { color: 'red', value: 1000 },
    ]),

    stat.new('Error Rate')
    + stat.panelOptions.withDescription('Combined errors across event exporter and Vector')
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

    stat.new('Ingestion Source')
    + stat.panelOptions.withDescription('Health of event ingestion source (apiserver or event-exporter)')
    + stat.options.withTextMode('value_and_name')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.ingestionSourceStatus)
      + prometheus.withLegendFormat('Status'),
    ])
    + stat.standardOptions.withMappings([
      { type: 'value', options: { '0': { text: 'Unhealthy', color: 'red' } } },
      { type: 'value', options: { '1': { text: 'Healthy', color: 'green' } } },
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'red', value: null },
      { color: 'green', value: 1 },
    ]),

    stat.new('Vector Buffer')
    + stat.panelOptions.withDescription('Events buffered in Vector before ClickHouse write')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.standardOptions.withUnit('short')
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.vectorEventsBufferDepth)
      + prometheus.withLegendFormat('Buffered'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'yellow', value: 100 },
      { color: 'red', value: 1000 },
    ]),

    stat.new('ClickHouse Insert Latency')
    + stat.panelOptions.withDescription('Average time to write events to k8s_events table')
    + stat.options.withGraphMode('area')
    + stat.options.withColorMode('background')
    + stat.standardOptions.withUnit('s')
    + stat.options.reduceOptions.withCalcs(['lastNotNull'])
    + stat.datasource.withType('prometheus')
    + stat.datasource.withUid('$datasource')
    + stat.queryOptions.withTargets([
      prometheus.new('$datasource', queries.clickhouseEventsInsertLatency)
      + prometheus.withLegendFormat('Insert Latency'),
    ])
    + stat.standardOptions.thresholds.withSteps([
      { color: 'green', value: null },
      { color: 'yellow', value: 0.1 },
      { color: 'orange', value: 0.5 },
      { color: 'red', value: 1.0 },
    ]),
  ], panelWidth=3, panelHeight=4, startY=1)

  // ============================================================================
  // Row 2: Event Ingestion
  // ============================================================================
  + [
    row.new('Event Ingestion')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(5),
  ]
  + util.grid.makeGrid([
    timeSeries.new('Events by Namespace')
    + timeSeries.panelOptions.withDescription('Top 10 namespaces generating events')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
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
      prometheus.new('$datasource', 'topk(10, ' + queries.eventsPublishedByNamespace + ')')
      + prometheus.withLegendFormat('{{namespace}}'),
    ]),

    timeSeries.new('Events by Reason')
    + timeSeries.panelOptions.withDescription('Common event reasons (Created, Scheduled, Pulling, etc.)')
    + timeSeries.options.legend.withDisplayMode('table')
    + timeSeries.options.legend.withPlacement('bottom')
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
      prometheus.new('$datasource', queries.eventsPublishedByReason)
      + prometheus.withLegendFormat('{{reason}}'),
    ]),

    timeSeries.new('Publish Latency (p50/p95/p99)')
    + timeSeries.panelOptions.withDescription('Latency distribution for publishing events to NATS')
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
      prometheus.new('$datasource', queries.publishLatencyP99)
      + prometheus.withLegendFormat('p99'),
      prometheus.new('$datasource', queries.publishLatencyP95)
      + prometheus.withLegendFormat('p95'),
      prometheus.new('$datasource', queries.publishLatencyP50)
      + prometheus.withLegendFormat('p50'),
    ]),
  ], panelWidth=8, panelHeight=7, startY=6)

  // ============================================================================
  // Row 3: Pipeline Flow
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
    timeSeries.new('Event Flow Through Stages')
    + timeSeries.panelOptions.withDescription('Events/sec at each pipeline stage - should be roughly equal in steady state')
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
      prometheus.new('$datasource', queries.eventsPublishedRate)
      + prometheus.withLegendFormat('1. Published to NATS'),
      prometheus.new('$datasource', queries.vectorNatsEventsReceived)
      + prometheus.withLegendFormat('2. Consumed from NATS'),
      prometheus.new('$datasource', queries.vectorClickhouseEventsSent)
      + prometheus.withLegendFormat('3. Sent to ClickHouse'),
      prometheus.new('$datasource', queries.clickhouseEventsWriteRate)
      + prometheus.withLegendFormat('4. ClickHouse Writes'),
    ]),

    timeSeries.new('Ingress vs Egress Comparison')
    + timeSeries.panelOptions.withDescription('Pipeline input vs output - gap indicates bottleneck or loss')
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
      prometheus.new('$datasource', queries.eventsPublishedRate)
      + prometheus.withLegendFormat('Ingress (Published)'),
      prometheus.new('$datasource', queries.clickhouseEventsWriteRate)
      + prometheus.withLegendFormat('Egress (ClickHouse)'),
    ]),
  ], panelWidth=12, panelHeight=8, startY=14)

  // ============================================================================
  // Row 4: Performance
  // ============================================================================
  + [
    row.new('Performance')
    + row.withCollapsed(false)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(22),
  ]
  + util.grid.makeGrid([
    timeSeries.new('NATS Consumer Lag')
    + timeSeries.panelOptions.withDescription('Pending messages for events consumer - indicates backpressure')
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
      prometheus.new('$datasource', queries.natsEventsQueuePending)
      + prometheus.withLegendFormat('Pending Messages'),
    ]),

    timeSeries.new('Vector Buffer Depth')
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
      prometheus.new('$datasource', queries.vectorEventsBufferDepth)
      + prometheus.withLegendFormat('Buffered Events'),
    ]),

    timeSeries.new('ClickHouse Insert Performance')
    + timeSeries.panelOptions.withDescription('Events insert rate and latency - write path health for k8s_events table')
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
      prometheus.new('$datasource', queries.clickhouseEventsWriteRate)
      + prometheus.withLegendFormat('Events Inserted/sec'),
    ]),

    timeSeries.new('Publish Errors Over Time')
    + timeSeries.panelOptions.withDescription('Errors publishing events to NATS - should be ZERO in healthy state')
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
      prometheus.new('$datasource', queries.publishErrors)
      + prometheus.withLegendFormat('Publish Errors'),
    ]),
  ], panelWidth=6, panelHeight=7, startY=23)

  // ============================================================================
  // Row 5: Error Breakdown (collapsed)
  // ============================================================================
  + [
    row.new('Error Breakdown')
    + row.withCollapsed(true)
    + row.gridPos.withH(1)
    + row.gridPos.withW(24)
    + row.gridPos.withX(0)
    + row.gridPos.withY(30)
    + row.withPanels(
      util.grid.makeGrid([
        timeSeries.new('Errors by Component')
        + timeSeries.panelOptions.withDescription('Breakdown of errors: Ingestion source vs Vector')
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
          prometheus.new('$datasource', queries.publishErrors)
          + prometheus.withLegendFormat('Ingestion Errors'),
          prometheus.new('$datasource', queries.vectorEventsErrors)
          + prometheus.withLegendFormat('Vector Errors'),
        ]),

        timeSeries.new('Vector Component Errors')
        + timeSeries.panelOptions.withDescription('Detailed Vector pipeline errors for events stream')
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
          prometheus.new('$datasource', 'sum(rate(vector_component_errors_total{component_id=~".*events.*"}[5m])) by (component_id) or vector(0)')
          + prometheus.withLegendFormat('{{component_id}}'),
        ]),
      ], panelWidth=12, panelHeight=8, startY=31)
    ),
  ];

// Dashboard
dashboard.new('Control Plane Events Pipeline')
+ dashboard.withDescription('End-to-end monitoring of K8s events: [Activity API Server|k8s-event-exporter] → NATS → Vector → ClickHouse. Works with both production (apiserver) and dev (event-exporter) environments.')
+ dashboard.withTags(['events', 'pipeline', 'activity', 'observability'])
+ dashboard.withUid('events-pipeline')
+ dashboard.time.withFrom(config.dashboards.timeRange.from)
+ dashboard.time.withTo(config.dashboards.timeRange.to)
+ dashboard.withTimezone(config.dashboards.timezone)
+ dashboard.withRefresh(refresh)
+ dashboard.withEditable(true)
+ dashboard.graphTooltip.withSharedCrosshair()
// TODO: Add cluster template variable when multi-cluster support is implemented
+ dashboard.withVariables([
  g.dashboard.variable.datasource.new('datasource', config.dashboards.datasource.type)
  + g.dashboard.variable.datasource.generalOptions.withLabel('Prometheus Datasource')
  + g.dashboard.variable.datasource.withRegex(datasourceRegex),
])
+ dashboard.withPanels(allPanels)
