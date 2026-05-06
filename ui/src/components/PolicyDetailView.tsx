import { useEffect, useCallback, useState } from 'react';
import type { ActivityPolicy, Condition } from '../types/policy';
import type { ResourceRef, ErrorFormatter } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { PolicyActivityView } from './PolicyActivityView';
import { PolicyActivityViewSkeleton } from './PolicyActivityViewSkeleton';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardHeader, CardContent } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';
import { ApiErrorAlert } from './ApiErrorAlert';
import { Alert, AlertDescription } from '@datum-cloud/datum-ui/alert';
import { AlertTriangle, AlertCircle, Copy, Check, Edit } from 'lucide-react';
import { Skeleton } from '@datum-cloud/datum-ui/skeleton';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from './ui/tooltip';

export interface PolicyDetailViewProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Policy name to display */
  policyName: string;
  /** Callback when edit button is clicked */
  onEdit?: () => void;
  /** Handler for resource link clicks in activity summaries */
  onResourceClick?: (resource: ResourceRef) => void;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

/**
 * PolicyDetailView displays a read-only view of an ActivityPolicy
 * with Activity/Events tabs and an Edit button
 */
export function PolicyDetailView({
  client,
  policyName,
  onEdit,
  onResourceClick,
  className = '',
  errorFormatter,
}: PolicyDetailViewProps) {
  const [policy, setPolicy] = useState<ActivityPolicy | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [isCopied, setIsCopied] = useState(false);

  // Load policy on mount
  useEffect(() => {
    let mounted = true;

    const loadPolicy = async () => {
      setIsLoading(true);
      setError(null);

      try {
        const loaded = await client.getPolicy(policyName);
        if (mounted) {
          setPolicy(loaded);
        }
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err : new Error('Failed to load policy'));
        }
      } finally {
        if (mounted) {
          setIsLoading(false);
        }
      }
    };

    loadPolicy();

    return () => {
      mounted = false;
    };
  }, [client, policyName]);

  // Handle copy resource name
  const handleCopyResourceName = useCallback(async () => {
    if (policyName) {
      try {
        await navigator.clipboard.writeText(policyName);
        setIsCopied(true);
        setTimeout(() => setIsCopied(false), 2000);
      } catch (err) {
        console.error('Failed to copy resource name:', err);
      }
    }
  }, [policyName]);

  // Get policy status from conditions
  const getPolicyStatus = (): {
    status: 'ready' | 'error' | 'pending' | 'unknown';
    message?: string;
  } | null => {
    if (!policy) {
      return null;
    }

    const conditions = policy.status?.conditions;
    if (!conditions || conditions.length === 0) {
      return null;
    }

    const readyCondition = conditions.find((c: Condition) => c.type === 'Ready');
    if (!readyCondition) {
      return null;
    }

    if (readyCondition.status === 'True') {
      return { status: 'ready', message: readyCondition.message || 'All rules compiled successfully' };
    } else if (readyCondition.status === 'False') {
      return { status: 'error', message: readyCondition.message || readyCondition.reason || 'Rule compilation failed' };
    }

    return { status: 'pending', message: readyCondition.message || 'Status unknown' };
  };

  const policyStatus = getPolicyStatus();
  const isUnhealthy = policyStatus && policyStatus.status !== 'ready';

  return (
    <TooltipProvider delayDuration={0}>
      <Card className={`rounded-xl ${className}`}>
        {/* Header */}
        <CardHeader className="flex flex-row justify-between items-center p-6 border-b border-border space-y-0">
          <div className="flex items-center gap-4">
            {isLoading && (
              <div className="flex flex-col gap-2">
                <div className="flex items-center gap-2">
                  <Skeleton className="w-2 h-2 rounded-full" />
                  <Skeleton className="h-6 w-32" />
                </div>
                <div className="flex items-center gap-2">
                  <Skeleton className="h-3 w-24" />
                  <Skeleton className="h-3 w-32" />
                </div>
              </div>
            )}
            {!isLoading && policy && (
              <div className="flex flex-col gap-1">
                <h2 className="m-0 text-xl font-semibold text-foreground leading-tight flex items-center gap-2">
                  <Tooltip delayDuration={500}>
                    <TooltipTrigger asChild>
                      <span
                        className={`w-2 h-2 rounded-full ${
                          policyStatus?.status === 'ready'
                            ? 'bg-green-500'
                            : policyStatus?.status === 'error'
                            ? 'bg-red-500'
                            : policyStatus?.status === 'pending'
                            ? 'bg-yellow-500'
                            : 'bg-green-500'
                        }`}
                      />
                    </TooltipTrigger>
                    <TooltipContent>
                      <p className="text-xs">
                        {policyStatus?.message || 'Policy is active'}
                      </p>
                    </TooltipContent>
                  </Tooltip>
                  {policy.spec.resource.kind || 'Untitled Policy'}
                </h2>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  {policy.spec.resource.apiGroup && (
                    <>
                      <span>API Group: {policy.spec.resource.apiGroup}</span>
                      <span className="text-muted-foreground/50">•</span>
                    </>
                  )}
                  <span>Resource: {policyName}</span>
                  <Tooltip delayDuration={500}>
                    <TooltipTrigger asChild>
                      <button
                        onClick={handleCopyResourceName}
                        className="inline-flex items-center justify-center p-0.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-opacity cursor-pointer"
                        aria-label="Copy resource name"
                      >
                        {isCopied ? (
                          <Check className="h-3 w-3 text-green-600 dark:text-green-400" />
                        ) : (
                          <Copy className="h-3 w-3 text-gray-500 dark:text-gray-400" />
                        )}
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p className="text-xs">Click to copy</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
            )}
          </div>

          <div className="flex gap-3">
            {onEdit && (
              <Button
                htmlType="button"
                size="small"
                onClick={onEdit}
                className="bg-primary text-primary-foreground border-primary hover:bg-primary/90 hover:border-primary/90 h-7 text-xs"
              >
                <Edit className="h-3.5 w-3.5 mr-1.5" />
                Edit Policy
              </Button>
            )}
          </div>
        </CardHeader>

        {/* Error Display */}
        <ApiErrorAlert error={error} className="mx-6 mt-4" errorFormatter={errorFormatter} />

        {/* Policy Health Status Banner */}
        {isUnhealthy && policyStatus && (
          <Alert
            variant={policyStatus.status === 'error' ? 'destructive' : 'warning'}
            className="mx-6 mt-4"
          >
            {policyStatus.status === 'error' ? (
              <AlertCircle className="h-4 w-4" />
            ) : (
              <AlertTriangle className="h-4 w-4" />
            )}
            <AlertDescription>
              <strong>
                {policyStatus.status === 'error' ? 'Policy Error: ' : 'Policy Pending: '}
              </strong>
              {policyStatus.message}
            </AlertDescription>
          </Alert>
        )}

        {/* Loading State */}
        {isLoading && (
          <CardContent className="p-6">
            <PolicyActivityViewSkeleton />
          </CardContent>
        )}

        {/* Main Content */}
        {!isLoading && policy && (
          <CardContent className="p-6">
            <PolicyActivityView
              client={client}
              policyResource={policy.spec.resource}
              policyName={policyName}
              onResourceClick={onResourceClick}
              errorFormatter={errorFormatter}
            />
          </CardContent>
        )}
      </Card>
    </TooltipProvider>
  );
}
