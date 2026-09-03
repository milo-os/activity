-- Migration: 010_source_metadata_columns
-- Description: Add source columns (plane type, cluster, region, city) to
-- k8s_events and activities, for federated event ingestion.
-- Columns default to empty string until source-* annotations are emitted.
-- Secondary indexes only - deliberately excluded from either table's sort key.
-- Author: Activity System
-- Date: 2026-09-02

-- ============================================================================
-- k8s_events: extracted from activity.miloapis.com/source-* annotations
-- ============================================================================

ALTER TABLE audit.k8s_events
    ADD COLUMN IF NOT EXISTS source_plane_type LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'metadata', 'annotations', 'activity.miloapis.com/source-plane-type'), '');

ALTER TABLE audit.k8s_events
    ADD COLUMN IF NOT EXISTS source_cluster LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'metadata', 'annotations', 'activity.miloapis.com/source-cluster'), '');

ALTER TABLE audit.k8s_events
    ADD COLUMN IF NOT EXISTS source_city LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'metadata', 'annotations', 'activity.miloapis.com/source-city'), '');

ALTER TABLE audit.k8s_events
    ADD COLUMN IF NOT EXISTS source_region LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(event_json, 'metadata', 'annotations', 'activity.miloapis.com/source-region'), '');

ALTER TABLE audit.k8s_events
    ADD INDEX IF NOT EXISTS idx_source_plane_type_set source_plane_type TYPE set(10) GRANULARITY 4;

ALTER TABLE audit.k8s_events
    ADD INDEX IF NOT EXISTS idx_source_cluster_bloom source_cluster TYPE bloom_filter(0.01) GRANULARITY 1;

ALTER TABLE audit.k8s_events
    ADD INDEX IF NOT EXISTS idx_source_city_set source_city TYPE set(100) GRANULARITY 4;

ALTER TABLE audit.k8s_events
    ADD INDEX IF NOT EXISTS idx_source_region_set source_region TYPE set(50) GRANULARITY 4;

-- ============================================================================
-- activities: extracted from ActivitySpec.Source
-- ============================================================================

ALTER TABLE audit.activities
    ADD COLUMN IF NOT EXISTS source_plane_type LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(activity_json, 'spec', 'source', 'planeType'), '');

ALTER TABLE audit.activities
    ADD COLUMN IF NOT EXISTS source_cluster LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(activity_json, 'spec', 'source', 'cluster'), '');

ALTER TABLE audit.activities
    ADD COLUMN IF NOT EXISTS source_city LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(activity_json, 'spec', 'source', 'city'), '');

ALTER TABLE audit.activities
    ADD COLUMN IF NOT EXISTS source_region LowCardinality(String) MATERIALIZED
        coalesce(JSONExtractString(activity_json, 'spec', 'source', 'region'), '');

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_source_plane_type_set source_plane_type TYPE set(10) GRANULARITY 4;

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_source_cluster_bloom source_cluster TYPE bloom_filter(0.01) GRANULARITY 1;

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_source_city_set source_city TYPE set(100) GRANULARITY 4;

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_source_region_set source_region TYPE set(50) GRANULARITY 4;
