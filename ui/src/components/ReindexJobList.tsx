import type { ErrorFormatter } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { useReindexJobs } from '../hooks/useReindexJobs';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardContent, CardHeader } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';
import { Separator } from '@datum-cloud/datum-ui/separator';
import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
import { ApiErrorAlert } from './ApiErrorAlert';
import { getReindexJobDuration } from '../types/reindex';
import { Clock, CheckCircle2, XCircle, Loader2, Eye } from 'lucide-react';

export interface ReindexJobListProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Callback when a job is selected for viewing */
  onViewJob?: (jobName: string) => void;
  /** Callback when create new job is requested */
  onCreateJob?: () => void;
  /** Whether to watch for real-time updates (default: true) */
  watch?: boolean;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

/**
 * Skeleton row for table loading state
 */
function ReindexJobSkeletonRow() {
  return (
    <tr>
      <td className="px-3 py-2 border-b border-border">
        <Skeleton className="h-4 w-32" />
      </td>
      <td className="px-3 py-2 border-b border-border text-center">
        <Skeleton className="h-5 w-16 mx-auto rounded-full" />
      </td>
      <td className="px-3 py-2 border-b border-border">
        <Skeleton className="h-4 w-full" />
      </td>
      <td className="px-3 py-2 border-b border-border text-center">
        <Skeleton className="h-4 w-16 mx-auto" />
      </td>
      <td className="px-3 py-2 border-b border-border text-center">
        <Skeleton className="h-7 w-16 mx-auto" />
      </td>
    </tr>
  );
}

/**
 * Get badge variant for job phase
 */
function getPhaseBadgeVariant(
  phase?: string
): 'default' | 'success' | 'destructive' | 'secondary' | 'warning' {
  switch (phase) {
    case 'Succeeded':
      return 'success';
    case 'Failed':
      return 'destructive';
    case 'Running':
      return 'default';
    case 'Pending':
      return 'secondary';
    default:
      return 'secondary';
  }
}

/**
 * Get icon for job phase
 */
function getPhaseIcon(phase?: string) {
  switch (phase) {
    case 'Succeeded':
      return <CheckCircle2 className="h-3.5 w-3.5" />;
    case 'Failed':
      return <XCircle className="h-3.5 w-3.5" />;
    case 'Running':
      return <Loader2 className="h-3.5 w-3.5 animate-spin" />;
    case 'Pending':
      return <Clock className="h-3.5 w-3.5" />;
    default:
      return <Clock className="h-3.5 w-3.5" />;
  }
}

/**
 * ReindexJobList displays all ReindexJobs with real-time updates
 */
export function ReindexJobList({
  client,
  onViewJob,
  onCreateJob,
  watch = true,
  className = '',
  errorFormatter,
}: ReindexJobListProps) {
  const { jobs, isLoading, error, refresh, isWatching } = useReindexJobs({
    client,
    watch,
    autoRefresh: true,
  });

  // Sort jobs by creation time (newest first)
  const sortedJobs = [...jobs].sort((a, b) => {
    const aTime = a.metadata?.creationTimestamp || '';
    const bTime = b.metadata?.creationTimestamp || '';
    return bTime.localeCompare(aTime);
  });

  return (
    <Card className={`rounded-xl ${className}`}>
      {/* Header */}
      <CardHeader className="pb-1.5">
        <div className="flex justify-between items-center">
          <div className="flex items-center gap-2">
            <p className="text-sm text-muted-foreground m-0">
              Re-process historical events through updated policies
            </p>
            {isWatching && (
              <Badge variant="success" className="text-xs">
                <span className="w-1.5 h-1.5 bg-green-500 rounded-full mr-1.5 animate-pulse" />
                Live
              </Badge>
            )}
          </div>
          <div className="flex gap-1.5">
            <Button
              type="tertiary" theme="outline"
              size="small"
              onClick={refresh}
              disabled={isLoading}
              title="Refresh job list"
              className="h-7 px-2"
            >
              {isLoading ? (
                <span className="w-3.5 h-3.5 border-2 border-border border-t-primary rounded-full animate-spin" />
              ) : (
                '↻'
              )}
            </Button>
            {onCreateJob && (
              <Button
                size="small"
                onClick={onCreateJob}
                className="bg-primary text-primary-foreground hover:bg-primary/90 h-7 text-xs"
              >
                + Create Job
              </Button>
            )}
          </div>
        </div>
        <Separator className="mt-1.5" />
      </CardHeader>

      <CardContent className="pt-3">
        {/* Error Display */}
        <ApiErrorAlert
          error={error}
          onRetry={refresh}
          className="mb-3"
          errorFormatter={errorFormatter}
        />

        {/* Loading State - Skeleton */}
        {isLoading && jobs.length === 0 && (
          <div className="border border-input rounded-lg overflow-hidden">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr>
                  <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap">
                    Job Name
                  </th>
                  <th className="text-center px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap w-24">
                    Status
                  </th>
                  <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap">
                    Message
                  </th>
                  <th className="text-center px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap w-24">
                    Duration
                  </th>
                  <th className="text-center px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap w-24">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                <ReindexJobSkeletonRow />
                <ReindexJobSkeletonRow />
                <ReindexJobSkeletonRow />
              </tbody>
            </table>
          </div>
        )}

        {/* Empty State */}
        {!isLoading && !error && jobs.length === 0 && (
          <div className="text-center py-8 px-6 text-muted-foreground">
            <div className="text-4xl mb-3">🔄</div>
            <h3 className="m-0 mb-1.5 text-foreground">No reindex jobs found</h3>
            <p className="m-0 mb-4 max-w-[400px] mx-auto">
              Create a reindex job to re-process historical audit logs and events
              through current ActivityPolicy rules.
            </p>
            {onCreateJob && (
              <Button
                size="small"
                onClick={onCreateJob}
                className="bg-primary text-primary-foreground hover:bg-primary/90 h-7 text-xs"
              >
                Create your first reindex job
              </Button>
            )}
          </div>
        )}

        {/* Jobs Table */}
        {sortedJobs.length > 0 && (
          <div className="border border-input rounded-lg overflow-hidden">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr>
                  <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap">
                    Job Name
                  </th>
                  <th className="text-center px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap w-24">
                    Status
                  </th>
                  <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap">
                    Message
                  </th>
                  <th className="text-center px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap w-24">
                    Duration
                  </th>
                  <th className="text-center px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap w-24">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                {sortedJobs.map((job) => {
                  const duration = getReindexJobDuration(job);

                  return (
                    <tr
                      key={job.metadata?.name}
                      className="hover:bg-muted transition-colors"
                    >
                      <td className="px-3 py-2 border-b border-border last:border-b-0">
                        <span className="text-xs font-medium font-mono">
                          {job.metadata?.name}
                        </span>
                      </td>
                      <td className="px-3 py-2 border-b border-border last:border-b-0 text-center">
                        <Badge
                          variant={getPhaseBadgeVariant(job.status?.phase)}
                          className="inline-flex items-center gap-1"
                        >
                          {getPhaseIcon(job.status?.phase)}
                          {job.status?.phase || 'Unknown'}
                        </Badge>
                      </td>
                      <td className="px-3 py-2 border-b border-border last:border-b-0">
                        {job.status?.phase === 'Failed' ? (
                          <span className="text-xs text-destructive">
                            {job.status?.message || 'Job failed'}
                          </span>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            {job.status?.message || 'Waiting to start'}
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2 border-b border-border last:border-b-0 text-center">
                        <span className="text-xs text-muted-foreground">
                          {duration || '-'}
                        </span>
                      </td>
                      <td className="px-3 py-2 border-b border-border last:border-b-0 text-center">
                        {onViewJob && (
                          <Button
                            type="quaternary" theme="borderless"
                            size="small"
                            onClick={() => onViewJob(job.metadata?.name || '')}
                            className="h-7 px-2"
                            title="View job details"
                          >
                            <Eye className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
