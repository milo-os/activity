/**
 * TypeScript types for the Activity API (activity.miloapis.com/v1alpha1)
 * Based on the Activity Stream System enhancement specification
 */

import type { ReactNode } from 'react';
import type { ObjectMeta } from './index';

/**
 * Change source classification for activities
 * - human: User actions, service accounts with `initiated-by: user` annotation
 * - system: Controller reconciliation, operator actions, scheduled jobs
 */
export type ChangeSource = 'human' | 'system';

/**
 * Actor types in the activity system
 */
export type ActorType = 'user' | 'machine account' | 'controller';

/**
 * Origin types for activity records
 */
export type OriginType = 'audit' | 'event';

/**
 * Tenant scope levels
 */
export type TenantType = 'platform' | 'organization' | 'project' | 'user';

/**
 * Resource reference for linking to resources in the portal
 */
export interface ResourceRef {
  apiGroup: string;
  apiVersion?: string;
  kind: string;
  name: string;
  namespace?: string;
  uid?: string;
}

/**
 * Context for resolving resource links
 * Contains optional tenant information for multi-tenant portal navigation
 */
export interface ResourceLinkContext {
  /** Current tenant context for scoped navigation */
  tenant?: Tenant;
}

/**
 * Actor who performed the action
 */
export interface Actor {
  type: ActorType;
  name: string;
  uid: string;
  email?: string;
  /**
   * Human-readable name (e.g. "Smith Nelson"). Populated server-side for
   * user actors when an iam User record is available. UIs SHOULD prefer
   * this over `name`/`email` for visible text and surface the email/UID on
   * hover.
   */
  displayName?: string;
}

/**
 * Linkable entity in the summary for portal rendering
 */
export interface ActivityLink {
  /** Text substring in summary that should be linked */
  marker: string;
  /** The resource to link to when the marker is clicked */
  resource: ResourceRef;
  /**
   * Human-readable name for the linked entity. Populated server-side for
   * User-typed links so the UI can render names instead of opaque UIDs.
   */
  displayName?: string;
  /**
   * Email address of the linked entity, when applicable (User links). The
   * UI surfaces this in a hover tooltip alongside the UID.
   */
  email?: string;
}

/**
 * Tenant scope for multi-tenant isolation
 */
export interface Tenant {
  type: TenantType;
  name: string;
  /** Unique identifier - required for user-scoped tenants where routes use ID instead of name */
  id?: string;
}

/**
 * Field-level change with old and new values
 */
export interface FieldChange {
  field: string;
  old?: string;
  new?: string;
}

/**
 * Origin of the activity record for correlation
 */
export interface ActivityOrigin {
  /** Source type: "audit" or "event" */
  type: OriginType;
  /** Correlation ID (auditID or event.metadata.uid) */
  id: string;
}

/**
 * Activity spec containing the translated activity data
 */
export interface ActivitySpec {
  /** Human-readable description generated from ActivityPolicy templates */
  summary: string;
  /** Who initiated the change: "human" or "system" */
  changeSource: ChangeSource;
  /** The actor who performed the action */
  actor: Actor;
  /** The resource that was affected by this activity */
  resource: ResourceRef;
  /** Linkable entities in the summary for portal rendering */
  links?: ActivityLink[];
  /** Tenant scope for multi-tenant isolation */
  tenant?: Tenant;
  /** Field-level changes with old and new values */
  changes?: FieldChange[];
  /** Origin of this activity record for correlation */
  origin: ActivityOrigin;
}

/**
 * Activity resource representing a single translated activity record
 */
export interface Activity {
  apiVersion: 'activity.miloapis.com/v1alpha1';
  kind: 'Activity';
  metadata: ObjectMeta;
  spec: ActivitySpec;
}

/**
 * Activity list response with pagination
 */
export interface ActivityList {
  apiVersion: 'activity.miloapis.com/v1alpha1';
  kind: 'ActivityList';
  metadata?: {
    continue?: string;
    resourceVersion?: string;
  };
  items: Activity[];
}

/**
 * Query parameters for listing activities
 */
export interface ActivityListParams {
  /** CEL expression to filter activities */
  filter?: string;
  /** Filter by field values (Kubernetes standard) */
  fieldSelector?: string;
  /** Filter by labels */
  labelSelector?: string;
  /** Full-text search on activity summaries */
  search?: string;
  /** Start of time range (RFC3339 or relative like "now-24h") */
  start?: string;
  /** End of time range (RFC3339 or relative, default: now) */
  end?: string;
  /** Maximum results (1-1000, default 100) */
  limit?: number;
  /** Pagination cursor */
  continue?: string;
  /** Watch for changes */
  watch?: boolean;
  /** Start Watch from this version */
  resourceVersion?: string;
}

/**
 * Facet specification for querying distinct values
 */
export interface FacetSpec {
  /** Field path to get distinct values for */
  field: string;
  /** Maximum values to return (1-100, default 20) */
  limit?: number;
}

/**
 * Facet value with count
 */
export interface FacetValue {
  value: string;
  count: number;
}

/**
 * Facet result containing distinct values for a field
 */
export interface FacetResult {
  field: string;
  values: FacetValue[];
}

/**
 * Activity facet query request spec
 */
export interface ActivityFacetQuerySpec {
  /** Time range for the facet query */
  timeRange?: {
    start?: string;
    end?: string;
  };
  /** CEL expression to filter activities before computing facets */
  filter?: string;
  /** List of facets to query (1-10 facets) */
  facets: FacetSpec[];
}

/**
 * Activity facet query status with results
 */
export interface ActivityFacetQueryStatus {
  facets: FacetResult[];
}

/**
 * Activity facet query resource
 */
export interface ActivityFacetQuery {
  apiVersion: 'activity.miloapis.com/v1alpha1';
  kind: 'ActivityFacetQuery';
  spec: ActivityFacetQuerySpec;
  status?: ActivityFacetQueryStatus;
}

/**
 * Audit log facet query request spec
 * Used to query distinct values from audit logs (API groups, resources, etc.)
 */
export interface AuditLogFacetsQuerySpec {
  /** Time range for the facet query */
  timeRange?: {
    start?: string;
    end?: string;
  };
  /** CEL expression to filter audit logs before computing facets */
  filter?: string;
  /** List of facets to query (1-10 facets) */
  facets: FacetSpec[];
}

/**
 * Audit log facet query status with results
 */
export interface AuditLogFacetsQueryStatus {
  facets: FacetResult[];
}

/**
 * Audit log facet query resource
 * Used to discover available API groups and resources from actual audit log data
 */
export interface AuditLogFacetsQuery {
  apiVersion: 'activity.miloapis.com/v1alpha1';
  kind: 'AuditLogFacetsQuery';
  spec: AuditLogFacetsQuerySpec;
  status?: AuditLogFacetsQueryStatus;
}

/**
 * Activity filter fields for autocomplete and help
 */
export interface ActivityFilterField {
  name: string;
  type: 'string' | 'enum' | 'timestamp';
  description: string;
  enumValues?: string[];
  examples?: string[];
}

/**
 * Available filter fields for activities
 */

/**
 * ActivityQuery spec for querying historical activities
 */
export interface ActivityQuerySpec {
  /** Start of time range (required) */
  startTime: string;
  /** End of time range (required) */
  endTime: string;
  /** Full-text search on summaries */
  search?: string;
  /** CEL filter expression (primary filtering mechanism) */
  filter?: string;
  /** Max results per page (default 100, max 1000) */
  limit?: number;
  /** Pagination cursor */
  continue?: string;
}

/**
 * ActivityQuery status with results
 */
export interface ActivityQueryStatus {
  /** Matching activities, newest first */
  results?: Activity[];
  /** Pagination cursor for next page */
  continue?: string;
  /** Resolved start time (RFC3339) */
  effectiveStartTime?: string;
  /** Resolved end time (RFC3339) */
  effectiveEndTime?: string;
}

/**
 * ActivityQuery resource for historical queries
 */
export interface ActivityQuery {
  apiVersion: 'activity.miloapis.com/v1alpha1';
  kind: 'ActivityQuery';
  metadata?: { name?: string };
  spec: ActivityQuerySpec;
  status?: ActivityQueryStatus;
}

/**
 * Watch event types from Kubernetes watch API
 */
export type WatchEventType = 'ADDED' | 'MODIFIED' | 'DELETED' | 'BOOKMARK' | 'ERROR';

/**
 * Watch event from the Kubernetes watch API
 */
export interface WatchEvent<T = Activity> {
  type: WatchEventType;
  object: T;
}

/**
 * Status error returned in watch ERROR events
 */
export interface WatchErrorStatus {
  apiVersion: 'v1';
  kind: 'Status';
  status: 'Failure';
  message: string;
  reason?: string;
  code: number;
}

/**
 * Function that resolves a ResourceRef to a navigation URL
 * Returns a URL string to navigate to when the resource link is clicked
 */
export type ResourceLinkResolver = (resource: ResourceRef, context?: ResourceLinkContext) => string | undefined;

/**
 * Default resource link resolver that navigates to resource history
 * Uses UID if available, otherwise falls back to apiGroup, kind, namespace, name
 */
export function defaultResourceLinkResolver(resource: ResourceRef, context: ResourceLinkContext = {}): string {
  const params = new URLSearchParams();
  if (resource.uid) {
    params.set('uid', resource.uid);
  } else {
    if (resource.apiGroup) params.set('apiGroup', resource.apiGroup);
    if (resource.kind) params.set('kind', resource.kind);
    if (resource.namespace) params.set('namespace', resource.namespace);
    if (resource.name) params.set('name', resource.name);
  }

  // Include tenant context if available
  if (context.tenant) {
    params.set('tenantType', context.tenant.type);
    params.set('tenantName', context.tenant.name);
  }

  return `/resource-history?${params.toString()}`;
}

/**
 * Function that resolves a Tenant to a navigation URL
 * Returns a URL string to navigate to when the tenant link is clicked, or undefined to not render as a link
 */
export type TenantLinkResolver = (tenant: Tenant) => string | undefined;

/**
 * Custom renderer for tenant badges
 * Allows full customization of how tenants are displayed in the activity feed
 */
export type TenantRenderer = (tenant: Tenant) => ReactNode;

/**
 * Effective time range after query resolution
 * Contains the actual start and end times used by the query (ISO 8601 format)
 */
export interface EffectiveTimeRange {
  /** Resolved start time in ISO 8601 format (e.g., "2026-02-19T10:00:00Z") */
  startTime: string;
  /** Resolved end time in ISO 8601 format (e.g., "2026-02-26T10:00:00Z") */
  endTime: string;
}

/**
 * Callback invoked when a query resolves its effective time range
 * Used by parent components to display or sync the actual time range used
 */
export type EffectiveTimeRangeCallback = (timeRange: EffectiveTimeRange) => void;

/**
 * Formatted error for display to users
 * Contains a user-friendly message and optional technical details
 */
export interface FormattedError {
  /** User-friendly error message */
  message: string;
  /** Technical details for advanced users */
  technical?: string;
}

/**
 * Function that formats errors for display
 * Allows clients to customize error messaging
 */
export type ErrorFormatter = (error: Error) => FormattedError;

export const ACTIVITY_FILTER_FIELDS: ActivityFilterField[] = [
  {
    name: 'spec.changeSource',
    type: 'enum',
    description: 'Change source classification',
    enumValues: ['human', 'system'],
    examples: [
      'spec.changeSource == "human"',
      'spec.changeSource == "system"',
    ],
  },
  {
    name: 'spec.actor.name',
    type: 'string',
    description: 'Actor display name',
    examples: [
      'spec.actor.name == "alice@example.com"',
      'spec.actor.name.startsWith("alice")',
    ],
  },
  {
    name: 'spec.actor.type',
    type: 'enum',
    description: 'Actor type',
    enumValues: ['user', 'machine account', 'controller'],
    examples: [
      'spec.actor.type == "user"',
      'spec.actor.type == "controller"',
    ],
  },
  {
    name: 'spec.resource.apiGroup',
    type: 'string',
    description: 'Resource API group',
    examples: [
      'spec.resource.apiGroup == "networking.datumapis.com"',
    ],
  },
  {
    name: 'spec.resource.kind',
    type: 'string',
    description: 'Resource kind',
    examples: [
      'spec.resource.kind == "HTTPProxy"',
      'spec.resource.kind in ["HTTPProxy", "Gateway"]',
    ],
  },
  {
    name: 'spec.resource.name',
    type: 'string',
    description: 'Resource name',
    examples: [
      'spec.resource.name == "api-gateway"',
      'spec.resource.name.contains("prod")',
    ],
  },
  {
    name: 'spec.resource.namespace',
    type: 'string',
    description: 'Resource namespace',
    examples: [
      'spec.resource.namespace == "default"',
    ],
  },
  {
    name: 'spec.resource.uid',
    type: 'string',
    description: 'Resource UID (for filtering to a specific resource)',
    examples: [
      'spec.resource.uid == "abc-123-def-456"',
    ],
  },
  {
    name: 'spec.origin.type',
    type: 'enum',
    description: 'Origin type',
    enumValues: ['audit', 'event'],
    examples: [
      'spec.origin.type == "audit"',
      'spec.origin.type == "event"',
    ],
  },
];
