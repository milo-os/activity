-- Single-node equivalent of audit.audit_logs as migrations 001 and 010 define
-- it. The replicated engines, projections and storage policies the production
-- schema uses need a ClickHouse cluster and Keeper; the column definitions here
-- are what the query path reads, so they are copied verbatim. The location
-- expression is asserted against migrations/010_audit_logs_location.sql by
-- TestSchemaFixtureMatchesMigration.

CREATE DATABASE IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS audit.audit_logs
(
    event_json String CODEC(ZSTD(3)),

    timestamp DateTime64(3) MATERIALIZED
        coalesce(
            parseDateTime64BestEffortOrNull(JSONExtractString(event_json, 'requestReceivedTimestamp')),
            now64(3)
        ),

    scope_type LowCardinality(String) MATERIALIZED
        coalesce(
            JSONExtractString(event_json, 'annotations', 'platform.miloapis.com/scope.type'),
            ''
        ),

    scope_name String MATERIALIZED
        coalesce(
            JSONExtractString(event_json, 'annotations', 'platform.miloapis.com/scope.name'),
            ''
        ),

    location LowCardinality(String) MATERIALIZED
        coalesce(
            JSONExtractString(event_json, 'annotations', 'locations.miloapis.com/location'),
            ''
        ),

    user String MATERIALIZED
        coalesce(JSONExtractString(event_json, 'user', 'username'), ''),

    user_uid String MATERIALIZED
        coalesce(JSONExtractString(event_json, 'user', 'uid'), ''),

    audit_id UUID MATERIALIZED
        toUUIDOrZero(coalesce(JSONExtractString(event_json, 'auditID'), '')),

    verb LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'verb'), ''),

    api_group LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'objectRef', 'apiGroup'), ''),

    resource LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'objectRef', 'resource'), ''),

    namespace LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'objectRef', 'namespace'), ''),

    resource_name String MATERIALIZED
        coalesce(JSONExtractString(event_json, 'objectRef', 'name'), ''),

    status_code UInt16 MATERIALIZED
        toUInt16OrZero(JSONExtractString(event_json, 'responseStatus', 'code'))
)
ENGINE = ReplacingMergeTree
ORDER BY (toStartOfHour(timestamp), timestamp, scope_type, scope_name, user, audit_id);
