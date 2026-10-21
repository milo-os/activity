// Activity SLO Recording Rules
// Pre-computed error ratios for multi-window burn-rate alerting
//
// SLO target: 99% availability / correctness (0.01 error budget)
//
// Five SLOs:
//   metadata        — activitypolicies GET/LIST/APPLY, latency < 1s (le="1")
//   audit_query     — auditlogqueries POST, latency < 3s (le="5" — no 3s bucket)
//   activity_query  — activityqueries + activityfacetqueries POST, latency < 3s (le="5")
//   event_query     — eventqueries + eventfacetqueries POST, latency < 3s (le="5")
//   availability    — all resources verb!="WATCH", non-5xx responses
//
// Recording rule naming convention:
//   activity:slo_<name>:request_good:rate5m   — good (fast or non-error) requests
//   activity:slo_<name>:request_total:rate5m  — total requests
//   activity:slo_<name>:request_total:rate1h  — total requests, for the page tier's volume floor
//   activity:slo_<name>:error_ratio:rate<W>   — 1 - (good / clamp_min(total, 1))
{
  prometheusRules+:: {
    groups+: [
      {
        name: 'activity-slo-recordings',
        interval: '30s',
        rules: [

          // =========================================================================
          // SLO: Metadata (activitypolicies GET/LIST/APPLY, latency < 1s)
          // =========================================================================

          // Good requests: completed within the 1s latency target
          {
            record: 'activity:slo_metadata:request_good:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_bucket{
                job="activity-apiserver",
                resource="activitypolicies",
                verb=~"GET|LIST|PATCH",
                le="1"
              }[5m]))
            |||,
          },

          // Total requests
          {
            record: 'activity:slo_metadata:request_total:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource="activitypolicies",
                verb=~"GET|LIST|PATCH"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_metadata:request_total:rate1h',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource="activitypolicies",
                verb=~"GET|LIST|PATCH"
              }[1h]))
            |||,
          },

          // Error ratios at each burn-rate window
          {
            record: 'activity:slo_metadata:error_ratio:rate5m',
            expr: |||
              (
                1 - (
                  activity:slo_metadata:request_good:rate5m
                  /
                  activity:slo_metadata:request_total:rate5m
                )
              ) and activity:slo_metadata:request_total:rate5m > 0
            |||,
          },

          {
            record: 'activity:slo_metadata:error_ratio:rate30m',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH",
                  le="1"
                }[30m]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[30m]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[30m])) > 0
            |||,
          },

          {
            record: 'activity:slo_metadata:error_ratio:rate1h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH",
                  le="1"
                }[1h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[1h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[1h])) > 0
            |||,
          },

          {
            record: 'activity:slo_metadata:error_ratio:rate6h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH",
                  le="1"
                }[6h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[6h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[6h])) > 0
            |||,
          },

          {
            record: 'activity:slo_metadata:error_ratio:rate3d',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH",
                  le="1"
                }[3d]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[3d]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="activitypolicies",
                  verb=~"GET|LIST|PATCH"
                }[3d])) > 0
            |||,
          },

          // =========================================================================
          // SLO: Audit Queries (auditlogqueries POST, latency < 3s; le="5" used —
          // no 3s histogram bucket is exposed by the apiserver)
          // =========================================================================

          {
            record: 'activity:slo_audit_query:request_good:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_bucket{
                job="activity-apiserver",
                resource="auditlogqueries",
                verb="POST",
                le="5"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_audit_query:request_total:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource="auditlogqueries",
                verb="POST"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_audit_query:request_total:rate1h',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource="auditlogqueries",
                verb="POST"
              }[1h]))
            |||,
          },

          {
            record: 'activity:slo_audit_query:error_ratio:rate5m',
            expr: |||
              (
                1 - (
                  activity:slo_audit_query:request_good:rate5m
                  /
                  activity:slo_audit_query:request_total:rate5m
                )
              ) and activity:slo_audit_query:request_total:rate5m > 0
            |||,
          },

          {
            record: 'activity:slo_audit_query:error_ratio:rate30m',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST",
                  le="5"
                }[30m]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[30m]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[30m])) > 0
            |||,
          },

          {
            record: 'activity:slo_audit_query:error_ratio:rate1h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST",
                  le="5"
                }[1h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[1h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[1h])) > 0
            |||,
          },

          {
            record: 'activity:slo_audit_query:error_ratio:rate6h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST",
                  le="5"
                }[6h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[6h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[6h])) > 0
            |||,
          },

          {
            record: 'activity:slo_audit_query:error_ratio:rate3d',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST",
                  le="5"
                }[3d]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[3d]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource="auditlogqueries",
                  verb="POST"
                }[3d])) > 0
            |||,
          },

          // =========================================================================
          // SLO: Activity Queries (activityqueries + activityfacetqueries POST,
          // latency < 3s; le="5" used — no 3s bucket)
          // =========================================================================

          {
            record: 'activity:slo_activity_query:request_good:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_bucket{
                job="activity-apiserver",
                resource=~"activityqueries|activityfacetqueries",
                verb="POST",
                le="5"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_activity_query:request_total:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource=~"activityqueries|activityfacetqueries",
                verb="POST"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_activity_query:request_total:rate1h',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource=~"activityqueries|activityfacetqueries",
                verb="POST"
              }[1h]))
            |||,
          },

          {
            record: 'activity:slo_activity_query:error_ratio:rate5m',
            expr: |||
              (
                1 - (
                  activity:slo_activity_query:request_good:rate5m
                  /
                  activity:slo_activity_query:request_total:rate5m
                )
              ) and activity:slo_activity_query:request_total:rate5m > 0
            |||,
          },

          {
            record: 'activity:slo_activity_query:error_ratio:rate30m',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST",
                  le="5"
                }[30m]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[30m]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[30m])) > 0
            |||,
          },

          {
            record: 'activity:slo_activity_query:error_ratio:rate1h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST",
                  le="5"
                }[1h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[1h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[1h])) > 0
            |||,
          },

          {
            record: 'activity:slo_activity_query:error_ratio:rate6h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST",
                  le="5"
                }[6h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[6h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[6h])) > 0
            |||,
          },

          {
            record: 'activity:slo_activity_query:error_ratio:rate3d',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST",
                  le="5"
                }[3d]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[3d]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"activityqueries|activityfacetqueries",
                  verb="POST"
                }[3d])) > 0
            |||,
          },

          // =========================================================================
          // SLO: Event Queries (eventqueries + eventfacetqueries POST,
          // latency < 3s; le="5" used — no 3s bucket)
          // =========================================================================

          {
            record: 'activity:slo_event_query:request_good:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_bucket{
                job="activity-apiserver",
                resource=~"eventqueries|eventfacetqueries",
                verb="POST",
                le="5"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_event_query:request_total:rate5m',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource=~"eventqueries|eventfacetqueries",
                verb="POST"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_event_query:request_total:rate1h',
            expr: |||
              sum(rate(apiserver_request_duration_seconds_count{
                job="activity-apiserver",
                resource=~"eventqueries|eventfacetqueries",
                verb="POST"
              }[1h]))
            |||,
          },

          {
            record: 'activity:slo_event_query:error_ratio:rate5m',
            expr: |||
              (
                1 - (
                  activity:slo_event_query:request_good:rate5m
                  /
                  activity:slo_event_query:request_total:rate5m
                )
              ) and activity:slo_event_query:request_total:rate5m > 0
            |||,
          },

          {
            record: 'activity:slo_event_query:error_ratio:rate30m',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST",
                  le="5"
                }[30m]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[30m]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[30m])) > 0
            |||,
          },

          {
            record: 'activity:slo_event_query:error_ratio:rate1h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST",
                  le="5"
                }[1h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[1h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[1h])) > 0
            |||,
          },

          {
            record: 'activity:slo_event_query:error_ratio:rate6h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST",
                  le="5"
                }[6h]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[6h]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[6h])) > 0
            |||,
          },

          {
            record: 'activity:slo_event_query:error_ratio:rate3d',
            expr: |||
              1 - (
                sum(rate(apiserver_request_duration_seconds_bucket{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST",
                  le="5"
                }[3d]))
                /
                sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[3d]))
              )
              and sum(rate(apiserver_request_duration_seconds_count{
                  job="activity-apiserver",
                  resource=~"eventqueries|eventfacetqueries",
                  verb="POST"
                }[3d])) > 0
            |||,
          },

          // =========================================================================
          // SLO: Availability (all resources verb!="WATCH", non-5xx responses)
          // =========================================================================

          // Good requests: any response that is not a 5xx server error
          {
            record: 'activity:slo_availability:request_good:rate5m',
            expr: |||
              sum(rate(apiserver_request_total{
                job="activity-apiserver",
                verb!="WATCH",
                code!~"5.."
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_availability:request_total:rate5m',
            expr: |||
              sum(rate(apiserver_request_total{
                job="activity-apiserver",
                verb!="WATCH"
              }[5m]))
            |||,
          },

          {
            record: 'activity:slo_availability:request_total:rate1h',
            expr: |||
              sum(rate(apiserver_request_total{
                job="activity-apiserver",
                verb!="WATCH"
              }[1h]))
            |||,
          },

          {
            record: 'activity:slo_availability:error_ratio:rate5m',
            expr: |||
              (
                1 - (
                  activity:slo_availability:request_good:rate5m
                  /
                  activity:slo_availability:request_total:rate5m
                )
              ) and activity:slo_availability:request_total:rate5m > 0
            |||,
          },

          {
            record: 'activity:slo_availability:error_ratio:rate30m',
            expr: |||
              1 - (
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH",
                  code!~"5.."
                }[30m]))
                /
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[30m]))
              )
              and sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[30m])) > 0
            |||,
          },

          {
            record: 'activity:slo_availability:error_ratio:rate1h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH",
                  code!~"5.."
                }[1h]))
                /
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[1h]))
              )
              and sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[1h])) > 0
            |||,
          },

          {
            record: 'activity:slo_availability:error_ratio:rate6h',
            expr: |||
              1 - (
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH",
                  code!~"5.."
                }[6h]))
                /
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[6h]))
              )
              and sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[6h])) > 0
            |||,
          },

          {
            record: 'activity:slo_availability:error_ratio:rate3d',
            expr: |||
              1 - (
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH",
                  code!~"5.."
                }[3d]))
                /
                sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[3d]))
              )
              and sum(rate(apiserver_request_total{
                  job="activity-apiserver",
                  verb!="WATCH"
                }[3d])) > 0
            |||,
          },

        ],
      },
    ],
  },
}
