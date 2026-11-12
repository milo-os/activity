-- Migration: 013_activities_interface_type
-- Description: Add interface_type to activities, backing the
-- spec.interface.type facet and filter. It holds ActivitySpec.Interface.Type:
-- how the change was made (console, cli, mcp, api, controller, terraform,
-- unknown), as reported by the client's user agent. Empty for activities
-- written before the field existed, until they are re-indexed.
-- Secondary index only - deliberately excluded from the sort key.
-- Author: Matt Jenkinson
-- Date: 2026-11-02

ALTER TABLE audit.activities
    ADD COLUMN IF NOT EXISTS interface_type LowCardinality(String) MATERIALIZED
        JSONExtractString(activity_json, 'spec', 'interface', 'type');

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_interface_type_set interface_type TYPE set(16) GRANULARITY 4;

ALTER TABLE audit.activities MATERIALIZE INDEX idx_interface_type_set;
