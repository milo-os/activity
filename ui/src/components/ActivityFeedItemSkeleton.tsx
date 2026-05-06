import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
import { cn } from '../lib/utils';

export interface ActivityFeedItemSkeletonProps {
  /** Whether to show as compact (for resource detail tabs) */
  compact?: boolean;
  /** Whether this is the last item in the list (hides bottom border) */
  isLast?: boolean;
  /** Additional CSS class */
  className?: string;
}

/**
 * Loading placeholder that mirrors the rendered shape of an
 * ActivityFeedItem in `variant="timeline"` mode: an action-icon square +
 * summary + tenant badge + timestamp + expand toggle, separated by the
 * same bottom border the live row uses.
 */
export function ActivityFeedItemSkeleton({
  compact = false,
  isLast = false,
  className = '',
}: ActivityFeedItemSkeletonProps) {
  return (
    <div className={cn(!isLast && 'border-b border-border', className)}>
      <div className={cn('flex items-center gap-3', compact ? 'py-2' : 'py-3')}>
        {/* Action icon square */}
        <Skeleton className="w-8 h-8 rounded-md shrink-0" />
        {/* Summary text */}
        <Skeleton className="h-4 flex-1 min-w-0" />
        {/* Tenant badge */}
        <Skeleton className="h-5 w-28 shrink-0 rounded-full" />
        {/* Timestamp */}
        <Skeleton className="h-4 w-24 shrink-0" />
        {/* Expand toggle */}
        <Skeleton className="h-5 w-5 shrink-0" />
      </div>
    </div>
  );
}
