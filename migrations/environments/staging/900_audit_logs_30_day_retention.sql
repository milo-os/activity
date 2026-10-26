-- Migration: 900_audit_logs_30_day_retention
-- Environment: staging only
-- Description: Keep 30 days of audit logs in staging and delete older days, instead
-- of moving them to cold storage after 90 days and keeping them forever.
-- materialize_ttl_after_modify = 0 stops ClickHouse from recomputing TTL on every
-- existing part, which would read the whole cold volume back from object storage.
-- Parts written before this runs keep no delete TTL and never expire on their own;
-- the operator drops those days with ALTER TABLE ... DROP PARTITION ID.
-- Author: Activity System
-- Date: 2026-10-26

SET materialize_ttl_after_modify = 0;

ALTER TABLE audit.audit_logs
    MODIFY TTL timestamp + INTERVAL 30 DAY DELETE;
