-- Migration: 014_activities_actor_type_index
-- Description: Index activities.actor_type so tenant-scoped audit log queries
-- can cheaply find the audit events that produced activities recorded as
-- staff access (actor_type = 'staff'), keeping Datum Support redaction sticky.
-- The lookup is also bounded by the activities primary key (timestamp,
-- tenant_type, tenant_name). Secondary index only.
-- Author: Matt Jenkinson
-- Date: 2026-11-02

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_actor_type_set actor_type TYPE set(16) GRANULARITY 4;

ALTER TABLE audit.activities MATERIALIZE INDEX idx_actor_type_set;
