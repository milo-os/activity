-- Migration: 010_audit_logs_location
-- Description: Materialize the location annotation on audit_logs so records
-- from edge control planes can be filtered and faceted by where they were
-- produced. Core control plane records leave the annotation unset and
-- materialize to an empty string.
-- Author: Activity System
-- Date: 2026-08-27

ALTER TABLE audit.audit_logs
    ADD COLUMN IF NOT EXISTS location LowCardinality(String) MATERIALIZED
        coalesce(
            JSONExtractString(event_json, 'annotations', 'locations.miloapis.com/location'),
            ''
        );

-- Set index: locations are a small, slow-growing set, so a set index prunes
-- granules for both equality and IN filters.
ALTER TABLE audit.audit_logs
    ADD INDEX IF NOT EXISTS idx_location_set location TYPE set(100) GRANULARITY 4;

ALTER TABLE audit.audit_logs MATERIALIZE COLUMN location;
ALTER TABLE audit.audit_logs MATERIALIZE INDEX idx_location_set;
