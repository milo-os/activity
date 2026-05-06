import { useEffect, useState, useCallback, useRef } from 'react';
import type { ReindexJob } from '../types/reindex';
import type { ErrorFormatter, WatchEvent } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardHeader, CardContent } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';
import { ApiErrorAlert } from './ApiErrorAlert';
import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
import {
  getReindexJobDuration,
  getReindexJobStatusMessage,
  isReindexJobTerminal,
} from '../types/reindex';
import {
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  Trash2,
} from 'lucide-react';

export interface ReindexJobDetailViewProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Job name to display */
  jobName: string;
  /** Callback when delete button is clicked */
  onDelete?: () => void;
  /** Whether to watch for real-time updates (default: true) */
  watch?: boolean;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

/**
 * Get icon for job phase
 */
function getPhaseIcon(phase?: string) {
  switch (phase) {
    case 'Succeeded':
      return <CheckCircle2 className="h-8 w-8 text-green-500" />;
    case 'Failed':
      return <XCircle className="h-8 w-8 text-red-500" />;
    case 'Running':
      return <Loader2 className="h-8 w-8 text-blue-500 animate-spin" />;
    case 'Pending':
    default:
      return <Clock className="h-8 w-8 text-muted-foreground" />;
  }
}

/**
 * ReindexJobDetailView displays status of a ReindexJob
 */
export function ReindexJobDetailView({
  client,
  jobName,
  onDelete,
  watch = true,
  className = '',
  errorFormatter,
}: ReindexJobDetailViewProps) {
  const [job, setJob] = useState<ReindexJob | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isWatching, setIsWatching] = useState(false);
  const resourceVersionRef = useRef<string | undefined>(undefined);

  const loadJob = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const loaded = await client.getReindexJob(jobName);
      setJob(loaded);
      resourceVersionRef.current = loaded.metadata?.resourceVersion;
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to load job'));
    } finally {
      setIsLoading(false);
    }
  }, [client, jobName]);

  const handleDelete = useCallback(async () => {
    if (!window.confirm(`Delete job "${jobName}"?`)) return;

    setIsDeleting(true);
    try {
      await client.deleteReindexJob(jobName);
      onDelete?.();
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to delete job'));
    } finally {
      setIsDeleting(false);
    }
  }, [client, jobName, onDelete]);

  useEffect(() => {
    loadJob();
  }, [loadJob]);

  // Watch for updates
  useEffect(() => {
    if (isLoading || !job || !watch) return;

    const isTerminalState = job.status?.phase === 'Succeeded' || job.status?.phase === 'Failed';
    if (isTerminalState) return;

    setIsWatching(true);
    const { stop } = client.watchReindexJobs({
      resourceVersion: resourceVersionRef.current,
      onEvent: (event: WatchEvent<ReindexJob>) => {
        if (event.object?.metadata?.name === jobName) {
          if (event.type === 'MODIFIED' || event.type === 'ADDED') {
            setJob(event.object);
            resourceVersionRef.current = event.object.metadata?.resourceVersion;
          } else if (event.type === 'DELETED') {
            onDelete?.();
          }
        }
      },
      onError: () => setIsWatching(false),
      onClose: () => setIsWatching(false),
    });

    return () => {
      stop();
      setIsWatching(false);
    };
  }, [client, jobName, watch, isLoading, job?.status?.phase, onDelete]);

  const duration = job ? getReindexJobDuration(job) : null;
  const statusMessage = job ? getReindexJobStatusMessage(job) : '';
  const isTerminal = job ? isReindexJobTerminal(job) : false;

  return (
    <Card className={`rounded-xl max-w-md ${className}`}>
      <CardHeader className="flex flex-row justify-between items-start p-6 border-b border-border space-y-0">
        <div className="flex items-center gap-4">
          {isLoading ? (
            <Skeleton className="h-8 w-8 rounded-full" />
          ) : (
            getPhaseIcon(job?.status?.phase)
          )}
          <div>
            {isLoading ? (
              <>
                <Skeleton className="h-5 w-32 mb-2" />
                <Skeleton className="h-3 w-24" />
              </>
            ) : (
              <>
                <h2 className="text-lg font-semibold text-foreground m-0">
                  {statusMessage}
                </h2>
                <p className="text-xs text-muted-foreground m-0">
                  {duration && <>{duration}</>}
                  {isWatching && !isTerminal && (
                    <Badge variant="outline" className="ml-2 text-xs text-green-600 border-green-300">
                      Live
                    </Badge>
                  )}
                </p>
              </>
            )}
          </div>
        </div>

        {isTerminal && onDelete && (
          <Button
            type="quaternary" theme="borderless"
            size="small"
            onClick={handleDelete}
            disabled={isDeleting}
            className="h-8 w-8 p-0"
          >
            {isDeleting ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="h-4 w-4" />
            )}
          </Button>
        )}
      </CardHeader>

      <ApiErrorAlert error={error} onRetry={loadJob} className="mx-6 mt-4" errorFormatter={errorFormatter} />

      {!isLoading && job && (
        <CardContent className="p-6 space-y-4">
          {/* Error message if failed */}
          {job.status?.phase === 'Failed' && job.status?.message && (
            <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
              {job.status.message}
            </div>
          )}

          {/* Time range */}
          <div>
            <div className="text-xs text-muted-foreground mb-1">Time Range</div>
            <div className="text-sm font-mono">
              {job.spec.timeRange.startTime}
              {job.spec.timeRange.endTime && <> → {job.spec.timeRange.endTime}</>}
            </div>
          </div>

          {/* Policy */}
          {job.spec.policySelector?.names && (
            <div>
              <div className="text-xs text-muted-foreground mb-1">Policy</div>
              <div className="flex flex-wrap gap-1">
                {job.spec.policySelector.names.map((name) => (
                  <Badge key={name} variant="secondary" className="text-xs font-mono">
                    {name}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      )}
    </Card>
  );
}
