import type { PolicyPreviewStatus, PreviewActivity, PolicyPreviewInputResult, PolicyPreviewInput } from '../types/policy';
import type { ResourceRef, Activity } from '../types/activity';
import type { K8sEvent } from '../types/k8s-event';
import { ActivityFeedSummary } from './ActivityFeedSummary';
import { ActivityFeedItem } from './ActivityFeedItem';
import { AuditLogFeedItem } from './AuditLogFeedItem';
import { EventFeedItem } from './EventFeedItem';
import { cn } from '../lib/utils';
import { Card, CardContent } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';
import { Alert, AlertDescription } from '@datum-cloud/datum-ui/alert';
import { AlertCircle, CheckCircle, XCircle } from 'lucide-react';

export interface PolicyPreviewResultProps {
  /** Preview result status */
  result: PolicyPreviewStatus;
  /** Input data used for preview */
  inputs?: PolicyPreviewInput[];
  /** Handler for resource link clicks */
  onResourceClick?: (resource: ResourceRef) => void;
  /** Additional CSS class */
  className?: string;
}

/**
 * Get summary statistics from results
 */
function getResultStats(results: PolicyPreviewInputResult[] | undefined): {
  total: number;
  matched: number;
  errors: number;
} {
  if (!results || results.length === 0) {
    return { total: 0, matched: 0, errors: 0 };
  }
  return {
    total: results.length,
    matched: results.filter((r) => r.matched).length,
    errors: results.filter((r) => !!r.error).length,
  };
}

/**
 * Convert PreviewActivity to Activity for ActivityFeedItem
 * PreviewActivity and Activity are similar but have slightly different actor types
 */
function previewActivityToActivity(preview: PreviewActivity): Activity {
  return {
    apiVersion: 'activity.miloapis.com/v1alpha1',
    kind: 'Activity',
    metadata: preview.metadata || {},
    spec: {
      summary: preview.spec.summary,
      changeSource: preview.spec.changeSource as 'human' | 'system',
      actor: {
        type: preview.spec.actor.type as 'user' | 'machine account' | 'controller',
        name: preview.spec.actor.name,
        uid: preview.spec.actor.uid || '',
        email: preview.spec.actor.email,
      },
      resource: {
        apiGroup: preview.spec.resource.apiGroup || '',
        apiVersion: preview.spec.resource.apiVersion,
        kind: preview.spec.resource.kind || '',
        name: preview.spec.resource.name || '',
        namespace: preview.spec.resource.namespace,
        uid: preview.spec.resource.uid,
      },
      links: preview.spec.links,
      origin: {
        type: preview.spec.origin.type as 'audit' | 'event',
        id: preview.spec.origin.id,
      },
    },
  };
}

/**
 * Convert KubernetesEvent (from policy.ts) to K8sEvent (from k8s-event.ts)
 * These types are very similar but defined separately in different files
 */
function policyEventToK8sEvent(policyEvent: PolicyPreviewInput['event']): K8sEvent {
  if (!policyEvent) {
    // Return a minimal K8sEvent if no event provided
    return {
      apiVersion: 'events.k8s.io/v1',
      kind: 'Event',
      metadata: {},
      regarding: {},
    };
  }

  return {
    apiVersion: 'events.k8s.io/v1',
    kind: 'Event',
    metadata: policyEvent.metadata || {},
    regarding: policyEvent.regarding || {},
    related: policyEvent.regarding,
    reason: policyEvent.reason,
    note: policyEvent.note,
    message: policyEvent.message,
    type: policyEvent.type as K8sEvent['type'],
    eventTime: policyEvent.eventTime,
    action: undefined,
    reportingController: policyEvent.reportingController,
    reportingInstance: policyEvent.reportingInstance,
    series: policyEvent.series,
    involvedObject: policyEvent.involvedObject,
    source: policyEvent.source,
    count: policyEvent.count,
    firstTimestamp: policyEvent.firstTimestamp,
    lastTimestamp: policyEvent.lastTimestamp,
  };
}

/**
 * PolicyPreviewResult displays the results of a policy preview execution
 */
export function PolicyPreviewResult({
  result,
  inputs = [],
  onResourceClick,
  className = '',
}: PolicyPreviewResultProps) {
  const hasError = !!result.error;
  const activities = result.activities || [];
  const results = result.results || [];
  const stats = getResultStats(results);

  // Handle legacy single-input response format
  const isLegacyFormat = !results.length && (result.matched !== undefined || result.generatedSummary);
  if (isLegacyFormat) {
    return (
      <Card className={className}>
        <CardContent className="pt-6">
          {/* Legacy Match Status Badge */}
          <div className="flex items-center gap-3 mb-4">
            {hasError ? (
              <Badge variant="destructive" className="gap-1">
                <XCircle className="h-3 w-3" />
                Error
              </Badge>
            ) : result.matched ? (
              <Badge variant="success" className="gap-1">
                <CheckCircle className="h-3 w-3" />
                Matched
              </Badge>
            ) : (
              <Badge variant="secondary" className="gap-1">
                <XCircle className="h-3 w-3" />
                No Match
              </Badge>
            )}

            {result.matched && !hasError && result.matchedRuleIndex !== undefined && (
              <span className="text-sm text-muted-foreground">
                {result.matchedRuleType === 'audit' ? 'Audit' : 'Event'} Rule #{result.matchedRuleIndex + 1}
              </span>
            )}
          </div>

          {hasError && (
            <Alert variant="destructive" className="mb-4">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                <pre className="text-xs font-mono whitespace-pre-wrap">{result.error}</pre>
              </AlertDescription>
            </Alert>
          )}

          {result.matched && result.generatedSummary && !hasError && (
            <div className="space-y-2">
              <p className="text-sm font-medium text-muted-foreground">Generated Summary:</p>
              <div className="p-3 rounded-md bg-muted">
                <ActivityFeedSummary
                  summary={result.generatedSummary}
                  links={result.generatedLinks}
                  onResourceClick={onResourceClick}
                />
              </div>
            </div>
          )}

          {!result.matched && !hasError && (
            <p className="text-sm text-muted-foreground">
              No rules matched the provided input. Check your match expressions.
            </p>
          )}
        </CardContent>
      </Card>
    );
  }

  // New multi-input format
  return (
    <div className={cn('space-y-3', className)}>
      {/* General Error */}
      {hasError && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>
            <pre className="text-xs font-mono whitespace-pre-wrap">{result.error}</pre>
          </AlertDescription>
        </Alert>
      )}

      {/* Activity Stream - using ActivityFeedItem for consistency */}
      {activities.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-medium text-foreground">Generated Activity Stream</h3>
            <Badge variant="outline">{activities.length} activities</Badge>
          </div>
          <div className="space-y-0">
            {activities.map((activity, index) => (
              <ActivityFeedItem
                key={activity.metadata?.name || index}
                activity={previewActivityToActivity(activity)}
                onResourceClick={onResourceClick}
                compact={true}
              />
            ))}
          </div>
        </div>
      )}

      {/* Per-Input Results (collapsed by default) */}
      {results.length > 0 && results.some((r) => !r.matched || r.error) && (
        <details className="group">
          <summary className="flex items-center gap-2 cursor-pointer text-sm text-muted-foreground hover:text-foreground transition-colors">
            <span className="group-open:rotate-90 transition-transform">
              ▶
            </span>
            {stats.total - stats.matched} inputs did not match
            {stats.errors > 0 && ` (${stats.errors} with errors)`}
          </summary>
          <Card className="mt-2">
            <CardContent className="p-0">
              <div className="divide-y divide-border">
                {results
                  .filter((r) => !r.matched || r.error)
                  .map((inputResult) => {
                    const input = inputs[inputResult.inputIndex];

                    return (
                      <div
                        key={inputResult.inputIndex}
                        className={cn(
                          'p-3 flex flex-col gap-2',
                          inputResult.error && 'bg-destructive/10'
                        )}
                      >
                        {/* Render the appropriate feed item component */}
                        {input?.type === 'audit' && input.audit && (
                          <AuditLogFeedItem event={input.audit} compact={true} />
                        )}
                        {input?.type === 'event' && input.event && (
                          <EventFeedItem event={policyEventToK8sEvent(input.event)} compact={true} />
                        )}
                        {!input && (
                          <span className="text-muted-foreground text-xs">
                            Input #{inputResult.inputIndex + 1}
                          </span>
                        )}

                        {/* Error or no-match indicator */}
                        <div className="flex justify-end">
                          {inputResult.error ? (
                            <Badge variant="destructive" className="text-xs shrink-0">
                              {inputResult.error}
                            </Badge>
                          ) : (
                            <span className="text-muted-foreground text-xs shrink-0">No matching rule</span>
                          )}
                        </div>
                      </div>
                    );
                  })}
              </div>
            </CardContent>
          </Card>
        </details>
      )}

      {/* No Matches */}
      {stats.matched === 0 && !hasError && (
        <Alert>
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>
            No rules matched any of the {stats.total} input{stats.total !== 1 ? 's' : ''}.
            Check your match expressions.
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
