-- Migration: 011_activities_related_uids
-- Description: Add related_uids to activities, backing the
-- `'<uid>' in spec.relatedUIDs` filter. Holds the distinct, non-empty UIDs of
-- spec.resource, spec.related[] and spec.links[].resource; keep in sync with
-- cel.RelatedUIDs.
-- Author: Activity System
-- Date: 2026-10-09

ALTER TABLE audit.activities
    ADD COLUMN IF NOT EXISTS related_uids Array(String) MATERIALIZED
        arrayFilter(uid -> uid != '', arrayDistinct(arrayConcat(
            [JSONExtractString(activity_json, 'spec', 'resource', 'uid')],
            arrayMap(r -> JSONExtractString(r, 'uid'), JSONExtractArrayRaw(activity_json, 'spec', 'related')),
            arrayMap(l -> JSONExtractString(l, 'resource', 'uid'), JSONExtractArrayRaw(activity_json, 'spec', 'links'))
        )));

ALTER TABLE audit.activities
    ADD INDEX IF NOT EXISTS idx_related_uids_bloom related_uids TYPE bloom_filter(0.001) GRANULARITY 1;

ALTER TABLE audit.activities MATERIALIZE INDEX idx_related_uids_bloom;
