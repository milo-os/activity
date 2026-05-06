import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
import { Card } from '@datum-cloud/datum-ui/card';

export interface PolicyActivityViewSkeletonProps {
  /** Additional CSS class */
  className?: string;
}

/**
 * PolicyActivityViewSkeleton renders a loading placeholder that matches PolicyActivityView layout
 */
export function PolicyActivityViewSkeleton({
  className = '',
}: PolicyActivityViewSkeletonProps) {
  return (
    <div className={className}>
      {/* Tab header skeleton - matches "Policy Events | Activity" order */}
      <div className="flex items-center gap-4 mb-4">
        <Skeleton className="h-5 w-24" />
        <span className="text-muted-foreground/30">|</span>
        <Skeleton className="h-5 w-16" />
      </div>

      {/* Filter bar skeleton */}
      <div className="flex items-center gap-2 mb-3">
        <Skeleton className="h-7 w-24" />
        <Skeleton className="h-7 w-20" />
        <Skeleton className="h-7 w-28" />
      </div>

      {/* Feed items skeleton */}
      <Card className="p-3">
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, index) => (
            <Card key={index} className="p-2">
              <div className="flex items-center gap-2">
                {/* Avatar skeleton */}
                <Skeleton className="w-4 h-4 rounded-full shrink-0" />
                {/* Summary skeleton */}
                <Skeleton className="h-3 flex-1" />
                {/* Timestamp skeleton */}
                <Skeleton className="h-3 w-16 shrink-0" />
                {/* Expand button skeleton */}
                <Skeleton className="h-5 w-5 shrink-0" />
              </div>
            </Card>
          ))}
        </div>
      </Card>
    </div>
  );
}
