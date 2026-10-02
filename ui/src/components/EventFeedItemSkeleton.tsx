import { Card } from '@datum-cloud/datum-ui/card';
import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
import { cn } from '../lib/utils';

export interface EventFeedItemSkeletonProps {
  /** Whether to show as compact (for resource detail tabs) */
  compact?: boolean;
  /** Additional CSS class */
  className?: string;
}

/**
 * EventFeedItemSkeleton renders a loading placeholder that matches EventFeedItem layout
 */
export function EventFeedItemSkeleton({
  compact = false,
  className = '',
}: EventFeedItemSkeletonProps) {
  return (
    <Card
      className={cn(
        compact ? 'p-2 mb-1.5' : 'p-2.5 mb-2',
        className
      )}
    >
      <div className="flex gap-2">
        {/* Main Content */}
        <div className="flex-1 min-w-0">
          {/* Single row layout: Message + Object + Timestamp + Expand */}
          <div className="flex items-center gap-2">
            {/* Note skeleton - takes remaining space */}
            <Skeleton className="h-3 flex-1 min-w-0" />

            {/* Regarding Object skeleton */}
            <Skeleton className="h-3 w-20" />

            {/* Timestamp skeleton */}
            <Skeleton className="h-3 w-16 shrink-0" />

            {/* Expand button skeleton */}
            <Skeleton className="h-5 w-5 shrink-0" />
          </div>
        </div>
      </div>
    </Card>
  );
}
