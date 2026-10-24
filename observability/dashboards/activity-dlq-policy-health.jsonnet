// Activity DLQ & Policy Health Grafana Dashboard
// Generated using Grafonnet v11.4.0
// To build: jsonnet -J vendor dashboards/activity-dlq-policy-health.jsonnet > ../config/components/observability/dashboards/generated/activity-dlq-policy-health.json

local g = import 'grafonnet-v11.4.0/main.libsonnet';
local config = import '../config.libsonnet';

local dashboard = g.dashboard;
local panel = g.panel;
local stat = panel.stat;
local timeSeries = panel.timeSeries;
local tablePanel = panel.table;
local logsPanel = panel.logs;
local textPanel = panel.text;
local row = panel.row;
local prometheus = g.query.prometheus;
local loki = g.query.loki;
local util = g.util;

local datasource = config.dashboards.datasource.name;
local datasourceRegex = config.dashboards.datasource.regex;
local refresh = config.dashboards.refresh;

local statHeight = 5;
local statWidth = 6;
local timeSeriesHeight = 8;
local timeSeriesHalfWidth = 12;
local tableHeight = 10;
local tableFullWidth = 24;

local SEL = 'cluster=~"$cluster", api_group=~"$api_group", kind=~"$kind", policy_name=~"$policy_name", error_type=~"$error_type"';
local SEL_retry_attempts = 'cluster=~"$cluster", api_group=~"$api_group", kind=~"$kind"';
local SEL_retry_failed = 'cluster=~"$cluster", api_group=~"$api_group", kind=~"$kind", policy_name=~"$policy_name", error_type=~"$error_type"';
local SEL_high_retry = 'cluster=~"$cluster", api_group=~"$api_group", kind=~"$kind", policy_name=~"$policy_name"';
local SEL_global = 'cluster=~"$cluster"';

local allPanels = util.grid.wrapPanels([
  row.new('At-a-Glance')
  + row.withCollapsed(false),

  stat.new('DLQ Backlog')
  + stat.options.withColorMode('background')
  + stat.options.withGraphMode('none')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('short')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'max(nats_stream_total_messages{stream_name="ACTIVITY_DEAD_LETTER", ' + SEL_global + '})'
    )
    + prometheus.withLegendFormat('Messages'),
  ])
  + stat.standardOptions.thresholds.withSteps([
    { color: 'green', value: null },
    { color: 'yellow', value: 100 },
    { color: 'red', value: 1000 },
  ])
  + stat.panelOptions.withDescription('Current number of events stuck in the DLQ')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  stat.new('Backlog Age (oldest)')
  + stat.options.withColorMode('value')
  + stat.options.withGraphMode('none')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('short')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'max(nats_stream_last_seq{stream_name="ACTIVITY_DEAD_LETTER", ' + SEL_global + '} - on() nats_stream_first_seq{stream_name="ACTIVITY_DEAD_LETTER", ' + SEL_global + '})'
    )
    + prometheus.withLegendFormat('Seq gap'),
  ])
  + stat.standardOptions.thresholds.withSteps([
    { color: 'green', value: null },
    { color: 'yellow', value: 1000 },
    { color: 'red', value: 10000 },
  ])
  + stat.panelOptions.withDescription('Sequence gap in DLQ stream — proxy for backlog age')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  stat.new('DLQ Publish Rate')
  + stat.options.withColorMode('background')
  + stat.options.withGraphMode('area')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('ops')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum(rate(activity_processor_dlq_events_published_total{' + SEL + '}[5m])) or vector(0)'
    )
    + prometheus.withLegendFormat('Events/s'),
  ])
  + stat.standardOptions.thresholds.withSteps([
    { color: 'green', value: null },
    { color: 'yellow', value: 0.1 },
    { color: 'red', value: 1 },
  ])
  + stat.panelOptions.withDescription('Rate of new events being published to the DLQ')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  stat.new('Retry Resolve Rate')
  + stat.options.withColorMode('value')
  + stat.options.withGraphMode('area')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('ops')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum(rate(activity_processor_dlq_retry_attempts_total{result=~"succeeded|republished", ' + SEL_retry_attempts + '}[5m])) or vector(0)'
    )
    + prometheus.withLegendFormat('Resolved/s'),
  ])
  + stat.panelOptions.withDescription('Rate at which retries are clearing DLQ events')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  stat.new('Net Drain')
  + stat.options.withColorMode('background')
  + stat.options.withGraphMode('none')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('ops')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      '(sum(rate(activity_processor_dlq_events_published_total{' + SEL + '}[5m])) or vector(0)) - (sum(rate(activity_processor_dlq_retry_attempts_total{result=~"succeeded|republished", ' + SEL_retry_attempts + '}[5m])) or vector(0))'
    )
    + prometheus.withLegendFormat('Net drain'),
  ])
  + stat.standardOptions.thresholds.withSteps([
    { color: 'green', value: null },
    { color: 'yellow', value: 0 },
    { color: 'red', value: 0.01 },
  ])
  + stat.panelOptions.withDescription('Publish rate minus resolve rate — positive means backlog is growing')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  stat.new('Retry Success Rate')
  + stat.options.withColorMode('background')
  + stat.options.withGraphMode('area')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('percentunit')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      '(sum(rate(activity_processor_dlq_retry_attempts_total{result="succeeded", ' + SEL_global + '}[5m])) or vector(0)) / clamp_min(sum(rate(activity_processor_dlq_retry_attempts_total{' + SEL_global + '}[5m])), 1)'
    )
    + prometheus.withLegendFormat('Success rate'),
  ])
  + stat.standardOptions.thresholds.withSteps([
    { color: 'red', value: null },
    { color: 'yellow', value: 0.8 },
    { color: 'green', value: 0.95 },
  ])
  + stat.panelOptions.withDescription('Fraction of DLQ retry attempts that succeeded')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  stat.new('DLQ Publish Errors')
  + stat.options.withColorMode('background')
  + stat.options.withGraphMode('area')
  + stat.options.reduceOptions.withCalcs(['lastNotNull'])
  + stat.standardOptions.withUnit('ops')
  + stat.datasource.withType('prometheus')
  + stat.datasource.withUid(datasource)
  + stat.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum(rate(activity_processor_dlq_publish_errors_total{' + SEL_global + '}[5m])) or vector(0)'
    )
    + prometheus.withLegendFormat('Errors/s'),
  ])
  + stat.standardOptions.thresholds.withSteps([
    { color: 'green', value: null },
    { color: 'red', value: 0.01 },
  ])
  + stat.panelOptions.withDescription('Rate of errors when publishing to DLQ — non-zero means events are being lost')
  + stat.gridPos.withW(statWidth)
  + stat.gridPos.withH(statHeight),

  row.new('What is broken NOW')
  + row.withCollapsed(false),

  tablePanel.new('Top Failing Policies')
  + tablePanel.datasource.withType('prometheus')
  + tablePanel.datasource.withUid(datasource)
  + tablePanel.options.withShowHeader(true)
  + tablePanel.options.withSortBy([
    { displayName: 'Value', desc: true },
  ])
  + tablePanel.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'topk(25, sum by (policy_name, api_group, kind, error_type) (rate(activity_processor_dlq_events_published_total{policy_name!="", ' + SEL + '}[10m])))'
    )
    + prometheus.withInstant(true)
    + prometheus.withLegendFormat('{{policy_name}}'),
  ])
  + tablePanel.standardOptions.withUnit('ops')
  + tablePanel.standardOptions.withLinks([
    {
      title: 'View in Loki',
      url: '/explore?orgId=1&left={"datasource":"loki","queries":[{"expr":"{namespace=\\"activity-system\\", container=\\"processor\\"} | json | policy=\\"${__data.fields.policy_name}\\" | errorType=~\\".+\\"","refId":"A"}],"range":{"from":"${__from}","to":"${__to}"}}',
      targetBlank: true,
    },
  ])
  + tablePanel.panelOptions.withDescription('Top 25 policies currently publishing to DLQ — the primary triage view for ActivityPolicyDLQErrors')
  + tablePanel.gridPos.withW(tableFullWidth)
  + tablePanel.gridPos.withH(tableHeight),

  row.new('Trends')
  + row.withCollapsed(false),

  timeSeries.new('DLQ Rate by error_type')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean'])
  + timeSeries.standardOptions.withUnit('ops')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum by (error_type) (rate(activity_processor_dlq_events_published_total{' + SEL + '}[5m]))'
    )
    + prometheus.withLegendFormat('{{error_type}}'),
  ])
  + timeSeries.panelOptions.withDescription('DLQ rate by failure class — identifies dominant error mode')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  timeSeries.new('DLQ Rate by policy')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean'])
  + timeSeries.standardOptions.withUnit('ops')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'label_replace(sum by (policy_name) (rate(activity_processor_dlq_events_published_total{' + SEL + '}[5m])), "policy_name", "(no policy)", "policy_name", "^$")'
    )
    + prometheus.withLegendFormat('{{policy_name}}'),
  ])
  + timeSeries.standardOptions.withLinks([
    {
      title: 'View in Loki',
      url: '/explore?orgId=1&left={"datasource":"loki","queries":[{"expr":"{namespace=\\"activity-system\\", container=\\"processor\\"} | json | policy=\\"${__field.labels.policy_name}\\"","refId":"A"}],"range":{"from":"${__from}","to":"${__to}"}}',
      targetBlank: true,
    },
  ])
  + timeSeries.panelOptions.withDescription('DLQ rate by policy — identifies persistent per-policy failures (DLQSlowLeak)')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  timeSeries.new('DLQ Rate by kind')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean'])
  + timeSeries.standardOptions.withUnit('ops')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum by (api_group, kind) (rate(activity_processor_dlq_events_published_total{' + SEL + '}[5m]))'
    )
    + prometheus.withLegendFormat('{{api_group}}/{{kind}}'),
  ])
  + timeSeries.panelOptions.withDescription('DLQ rate by resource kind — identifies affected resource types')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  row.new('Retry & Recovery')
  + row.withCollapsed(false),

  timeSeries.new('Retry outcomes')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean'])
  + timeSeries.standardOptions.withUnit('ops')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum by (result) (rate(activity_processor_dlq_retry_attempts_total{' + SEL_retry_attempts + '}[5m]))'
    )
    + prometheus.withLegendFormat('{{result}}'),
  ])
  + timeSeries.panelOptions.withDescription('Retry attempt outcomes over time — succeeded vs republished vs failed')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  tablePanel.new('Still-failing re-eval by policy')
  + tablePanel.datasource.withType('prometheus')
  + tablePanel.datasource.withUid(datasource)
  + tablePanel.options.withShowHeader(true)
  + tablePanel.options.withSortBy([
    { displayName: 'Value', desc: true },
  ])
  + tablePanel.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'topk(25, sum by (policy_name, error_type) (rate(activity_processor_dlq_retry_failed_total{' + SEL_retry_failed + '}[10m])))'
    )
    + prometheus.withInstant(true)
    + prometheus.withLegendFormat('{{policy_name}}'),
  ])
  + tablePanel.standardOptions.withUnit('ops')
  + tablePanel.panelOptions.withDescription('Policies NOT recovering after retry — triage for DLQRetryIneffective')
  + tablePanel.gridPos.withW(timeSeriesHalfWidth)
  + tablePanel.gridPos.withH(timeSeriesHeight),

  timeSeries.new('High-retry (poison) events by policy')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean'])
  + timeSeries.standardOptions.withUnit('short')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum by (policy_name, api_group, kind) (increase(activity_processor_dlq_retry_events_high_retry_total{' + SEL_high_retry + '}[1h]))'
    )
    + prometheus.withLegendFormat('{{policy_name}}'),
  ])
  + timeSeries.panelOptions.withDescription('Events exceeding retry threshold by policy — identifies poison events (DLQHighRetryCount)')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  timeSeries.new('Retry batch duration p99')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean', 'max'])
  + timeSeries.standardOptions.withUnit('s')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'histogram_quantile(0.99, sum by (le, trigger) (rate(activity_processor_dlq_retry_batch_duration_seconds_bucket{' + SEL_global + '}[5m])))'
    )
    + prometheus.withLegendFormat('{{trigger}} p99'),
  ])
  + timeSeries.panelOptions.withDescription('Retry batch processing duration — high values indicate retry path stalling')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  row.new('Publish-Path Health')
  + row.withCollapsed(false),

  timeSeries.new('Publish errors by phase')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean'])
  + timeSeries.standardOptions.withUnit('ops')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(30)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.fieldConfig.defaults.custom.stacking.withMode('normal')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'sum by (error_phase) (rate(activity_processor_dlq_publish_errors_total{' + SEL_global + '}[5m]))'
    )
    + prometheus.withLegendFormat('{{error_phase}}'),
  ])
  + timeSeries.panelOptions.withDescription('DLQ publish errors by phase (marshal/publish) — non-zero is data loss risk')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  timeSeries.new('DLQ publish latency')
  + timeSeries.options.legend.withDisplayMode('table')
  + timeSeries.options.legend.withPlacement('bottom')
  + timeSeries.options.legend.withShowLegend(true)
  + timeSeries.options.legend.withCalcs(['lastNotNull', 'mean', 'max'])
  + timeSeries.standardOptions.withUnit('s')
  + timeSeries.fieldConfig.defaults.custom.withFillOpacity(10)
  + timeSeries.fieldConfig.defaults.custom.withShowPoints('never')
  + timeSeries.datasource.withType('prometheus')
  + timeSeries.datasource.withUid(datasource)
  + timeSeries.queryOptions.withTargets([
    prometheus.new(
      datasource,
      'histogram_quantile(0.99, sum by (le) (rate(activity_processor_dlq_publish_latency_seconds_bucket{' + SEL_global + '}[5m])))'
    )
    + prometheus.withLegendFormat('p99'),
    prometheus.new(
      datasource,
      'histogram_quantile(0.95, sum by (le) (rate(activity_processor_dlq_publish_latency_seconds_bucket{' + SEL_global + '}[5m])))'
    )
    + prometheus.withLegendFormat('p95'),
    prometheus.new(
      datasource,
      'histogram_quantile(0.50, sum by (le) (rate(activity_processor_dlq_publish_latency_seconds_bucket{' + SEL_global + '}[5m])))'
    )
    + prometheus.withLegendFormat('p50'),
  ])
  + timeSeries.panelOptions.withDescription('DLQ publish write path latency distribution')
  + timeSeries.gridPos.withW(timeSeriesHalfWidth)
  + timeSeries.gridPos.withH(timeSeriesHeight),

  row.new('Processor Logs')
  + row.withCollapsed(false),

  logsPanel.new('DLQ Events — Processor Logs')
  + logsPanel.datasource.withType('loki')
  + logsPanel.datasource.withUid('$loki_datasource')
  + logsPanel.options.withShowTime(true)
  + logsPanel.options.withSortOrder('Descending')
  + logsPanel.options.withWrapLogMessage(false)
  + logsPanel.options.withEnableLogDetails(true)
  + logsPanel.options.withDisplayedFields(['policy', 'errorType', 'msg'])
  + logsPanel.queryOptions.withTargets([
    loki.new(
      '$loki_datasource',
      '{namespace="activity-system", container="processor"} | json | errorType != "" | policy=~"${policy_name:regex}" | errorType=~"${error_type:regex}"'
    )
    + loki.withRefId('A'),
  ])
  + logsPanel.panelOptions.withDescription('DLQ processor logs filtered by selected policy and error type — shows Published event to DLQ lines with policy and errorType fields')
  + logsPanel.gridPos.withW(tableFullWidth)
  + logsPanel.gridPos.withH(tableHeight),

  logsPanel.new('DLQ/Policy Errors (raw)')
  + logsPanel.datasource.withType('loki')
  + logsPanel.datasource.withUid('$loki_datasource')
  + logsPanel.options.withShowTime(true)
  + logsPanel.options.withSortOrder('Descending')
  + logsPanel.options.withWrapLogMessage(false)
  + logsPanel.options.withEnableLogDetails(true)
  + logsPanel.queryOptions.withTargets([
    loki.new(
      '$loki_datasource',
      '{namespace="activity-system", container="processor"} |~ "(?i)dlq|dead.letter|failed to evaluate|failed to republish|Published event to DLQ"'
    )
    + loki.withRefId('A'),
  ])
  + logsPanel.panelOptions.withDescription('Catch-all DLQ log filter — includes non-JSON error lines and all dlq/dead-letter references')
  + logsPanel.gridPos.withW(tableFullWidth)
  + logsPanel.gridPos.withH(tableHeight),
], panelWidth=statWidth, panelHeight=statHeight);

dashboard.new('Activity — DLQ & Policy Health')
+ dashboard.withDescription('Single-pane triage dashboard for DLQ backlog, failing policies, retry recovery, and processor logs')
+ dashboard.withTags(['activity', 'dlq', 'policy', 'health', 'on-call'])
+ dashboard.withUid('activity-dlq-policy-health')
+ dashboard.time.withFrom('now-6h')
+ dashboard.time.withTo('now')
+ dashboard.withTimezone(config.dashboards.timezone)
+ dashboard.withRefresh(refresh)
+ dashboard.withEditable(true)
+ dashboard.graphTooltip.withSharedCrosshair()
+ dashboard.withLinks([
  {
    title: 'DLQ Runbooks',
    url: 'https://github.com/milo-os/activity/tree/main/docs/runbooks/dlq/',
    type: 'link',
    targetBlank: true,
    icon: 'external link',
  },
])
+ dashboard.withVariablesMixin([
  g.dashboard.variable.datasource.new('datasource', 'prometheus')
  + g.dashboard.variable.datasource.generalOptions.withLabel('Prometheus Datasource')
  + g.dashboard.variable.datasource.withRegex(datasourceRegex),

  g.dashboard.variable.query.new('cluster', 'label_values(activity_processor_dlq_events_published_total, cluster)')
  + g.dashboard.variable.query.withDatasource('prometheus', datasource)
  + g.dashboard.variable.query.generalOptions.withLabel('Cluster')
  + g.dashboard.variable.query.selectionOptions.withMulti()
  + g.dashboard.variable.query.selectionOptions.withIncludeAll()
  + g.dashboard.variable.query.refresh.onTime(),

  g.dashboard.variable.query.new('api_group', 'label_values(activity_processor_dlq_events_published_total{cluster=~"$cluster"}, api_group)')
  + g.dashboard.variable.query.withDatasource('prometheus', datasource)
  + g.dashboard.variable.query.generalOptions.withLabel('API Group')
  + g.dashboard.variable.query.selectionOptions.withMulti()
  + g.dashboard.variable.query.selectionOptions.withIncludeAll()
  + g.dashboard.variable.query.refresh.onTime(),

  g.dashboard.variable.query.new('kind', 'label_values(activity_processor_dlq_events_published_total{cluster=~"$cluster",api_group=~"$api_group"}, kind)')
  + g.dashboard.variable.query.withDatasource('prometheus', datasource)
  + g.dashboard.variable.query.generalOptions.withLabel('Kind')
  + g.dashboard.variable.query.selectionOptions.withMulti()
  + g.dashboard.variable.query.selectionOptions.withIncludeAll()
  + g.dashboard.variable.query.refresh.onTime(),

  g.dashboard.variable.query.new('policy_name', 'label_values(activity_processor_dlq_events_published_total{cluster=~"$cluster",api_group=~"$api_group",kind=~"$kind"}, policy_name)')
  + g.dashboard.variable.query.withDatasource('prometheus', datasource)
  + g.dashboard.variable.query.generalOptions.withLabel('Policy')
  + g.dashboard.variable.query.selectionOptions.withMulti()
  + g.dashboard.variable.query.selectionOptions.withIncludeAll()
  + g.dashboard.variable.query.refresh.onTime(),

  g.dashboard.variable.query.new('error_type', 'label_values(activity_processor_dlq_events_published_total{cluster=~"$cluster"}, error_type)')
  + g.dashboard.variable.query.withDatasource('prometheus', datasource)
  + g.dashboard.variable.query.generalOptions.withLabel('Error Type')
  + g.dashboard.variable.query.selectionOptions.withMulti()
  + g.dashboard.variable.query.selectionOptions.withIncludeAll()
  + g.dashboard.variable.query.refresh.onTime(),

  g.dashboard.variable.datasource.new('loki_datasource', 'loki')
  + g.dashboard.variable.datasource.generalOptions.withLabel('Loki Datasource')
  + g.dashboard.variable.datasource.generalOptions.showOnDashboard.withNothing(),
])
+ dashboard.withPanels(allPanels)
