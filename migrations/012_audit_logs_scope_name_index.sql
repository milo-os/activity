-- Migration: 012_audit_logs_scope_name_index
-- Description: Add a bloom filter skip index on scope_name to audit_logs, in the shape of
-- idx_resource_bloom. The sort key puts the exact timestamp before scope_name, so the
-- primary key cannot skip a granule for a project- or organization-scoped search; this
-- index skips the granules that hold none of the scope's rows.
-- 0.01, not k8s_events' 0.001: 0.001 makes the index 50% larger and skips only a few more granules.
-- Author: Activity System
-- Date: 2026-10-21

ALTER TABLE audit.audit_logs
    ADD INDEX IF NOT EXISTS idx_scope_name_bloom scope_name TYPE bloom_filter(0.01) GRANULARITY 1;

-- Not materialized for existing data: parts older than 90 days live on the cold volume, and
-- a whole-table MATERIALIZE INDEX rewrites them. New parts get the index on insert and on
-- merge, so 30-day searches are covered 30 days after this runs. Recent data can be covered
-- sooner per day: ALTER TABLE audit.audit_logs MATERIALIZE INDEX idx_scope_name_bloom IN PARTITION <YYYYMMDD>;
