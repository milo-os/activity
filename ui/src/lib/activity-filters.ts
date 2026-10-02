import type { ActivityFeedFilters, TimeRange } from '../hooks/useActivityFeed';
import type { EventsFeedFilters } from '../hooks/useEventsFeed';

/**
 * Serialize Activity Feed filters to URL search params.
 * Used to sync filter changes back to the URL for deep linking.
 */
export function serializeActivityFilters(
  filters: Partial<ActivityFeedFilters>,
  timeRange: TimeRange,
  streamingEnabled: boolean = true
): URLSearchParams {
  const params = new URLSearchParams();

  if (timeRange.start) {
    params.set('start', timeRange.start);
  }
  if (timeRange.end) {
    params.set('end', timeRange.end);
  }

  if (!streamingEnabled) {
    params.set('streaming', 'false');
  }

  // Written even when it matches a default: the default differs between
  // pages, so an omitted value can't be read back as the one chosen.
  if (filters.changeSource) {
    params.set('changeSource', filters.changeSource);
  }

  if (filters.actorNames && filters.actorNames.length > 0) {
    params.set('actorNames', filters.actorNames.join(','));
  }

  if (filters.resourceKinds && filters.resourceKinds.length > 0) {
    params.set('resourceKinds', filters.resourceKinds.join(','));
  }

  if (filters.apiGroups && filters.apiGroups.length > 0) {
    params.set('apiGroups', filters.apiGroups.join(','));
  }

  if (filters.resourceNamespaces && filters.resourceNamespaces.length > 0) {
    params.set('resourceNamespaces', filters.resourceNamespaces.join(','));
  }

  if (filters.resourceUid) {
    params.set('resourceUid', filters.resourceUid);
  }

  if (filters.resourceName) {
    params.set('resourceName', filters.resourceName);
  }

  if (filters.search) {
    params.set('search', filters.search);
  }

  return params;
}

/**
 * Serialize Events Feed filters to URL search params.
 * Used to sync filter changes back to the URL for deep linking.
 */
export function serializeEventFilters(
  filters: Partial<EventsFeedFilters>,
  timeRange: TimeRange,
  streamingEnabled: boolean = true
): URLSearchParams {
  const params = new URLSearchParams();

  if (timeRange.start) {
    params.set('start', timeRange.start);
  }
  if (timeRange.end) {
    params.set('end', timeRange.end);
  }

  if (!streamingEnabled) {
    params.set('streaming', 'false');
  }

  if (filters.eventType && filters.eventType !== 'all') {
    params.set('eventType', filters.eventType);
  }

  if (filters.reasons && filters.reasons.length > 0) {
    params.set('reasons', filters.reasons.join(','));
  }

  if (filters.namespaces && filters.namespaces.length > 0) {
    params.set('namespaces', filters.namespaces.join(','));
  }

  if (filters.involvedKinds && filters.involvedKinds.length > 0) {
    params.set('involvedKinds', filters.involvedKinds.join(','));
  }

  if (filters.search) {
    params.set('search', filters.search);
  }

  return params;
}

/**
 * Parse Activity Feed filters from URL query params
 */
export function parseActivityFilters(searchParams: URLSearchParams): Partial<ActivityFeedFilters> {
  const filters: Partial<ActivityFeedFilters> = {};

  const changeSource = searchParams.get('changeSource');
  // Left unset when absent so the caller's default applies. Filling in
  // 'human' here overrode pages that default to 'all'.
  if (changeSource === 'human' || changeSource === 'system' || changeSource === 'all') {
    filters.changeSource = changeSource;
  }

  const actorNames = searchParams.get('actorNames');
  if (actorNames) {
    filters.actorNames = actorNames.split(',').filter(Boolean);
  }

  const resourceKinds = searchParams.get('resourceKinds');
  if (resourceKinds) {
    filters.resourceKinds = resourceKinds.split(',').filter(Boolean);
  }

  const apiGroups = searchParams.get('apiGroups');
  if (apiGroups) {
    filters.apiGroups = apiGroups.split(',').filter(Boolean);
  }

  const resourceNamespaces = searchParams.get('resourceNamespaces');
  if (resourceNamespaces) {
    filters.resourceNamespaces = resourceNamespaces.split(',').filter(Boolean);
  }

  const resourceUid = searchParams.get('resourceUid');
  if (resourceUid) {
    filters.resourceUid = resourceUid;
  }

  const resourceName = searchParams.get('resourceName');
  if (resourceName) {
    filters.resourceName = resourceName;
  }

  const search = searchParams.get('search');
  if (search) {
    filters.search = search;
  }

  return filters;
}

/**
 * Parse Events Feed filters from URL query params
 */
export function parseEventFilters(searchParams: URLSearchParams): Partial<EventsFeedFilters> {
  const filters: Partial<EventsFeedFilters> = {};

  const eventType = searchParams.get('eventType');
  if (eventType === 'Normal' || eventType === 'Warning' || eventType === 'all') {
    filters.eventType = eventType;
  }

  const reasons = searchParams.get('reasons');
  if (reasons) {
    filters.reasons = reasons.split(',').filter(Boolean);
  }

  const namespaces = searchParams.get('namespaces');
  if (namespaces) {
    filters.namespaces = namespaces.split(',').filter(Boolean);
  }

  const involvedKinds = searchParams.get('involvedKinds');
  if (involvedKinds) {
    filters.involvedKinds = involvedKinds.split(',').filter(Boolean);
  }

  const search = searchParams.get('search');
  if (search) {
    filters.search = search;
  }

  return filters;
}

/**
 * Parse time range from URL query params
 */
export function parseTimeRange(searchParams: URLSearchParams): TimeRange | undefined {
  const start = searchParams.get('start');
  const end = searchParams.get('end');

  if (!start) {
    return undefined;
  }

  return { start, end: end || undefined };
}
