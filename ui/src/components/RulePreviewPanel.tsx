import { useEffect, useState, useRef } from 'react';
import type { ActivityPolicyRule, PolicyPreviewInput } from '../types/policy';
import type { ActivityApiClient } from '../api/client';
import type { K8sEvent } from '../types/k8s-event';
import type { PolicyPreviewStatus } from '../types/policy';
import { Card, CardContent } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';
import { Alert, AlertDescription } from '@datum-cloud/datum-ui/alert';
import { AlertCircle, CheckCircle, XCircle, Loader2 } from 'lucide-react';
import { AuditLogFeedItem } from './AuditLogFeedItem';
import { EventFeedItem } from './EventFeedItem';
import { ActivityFeedSummary } from './ActivityFeedSummary';
import { cn } from '../lib/utils';

export interface RulePreviewPanelProps {
  /** The rule to preview */
  rule: ActivityPolicyRule;
  /** Rule type (audit or event) */
  ruleType: 'audit' | 'event';
  /** Policy resource (apiGroup/kind) for fetching sample data */
  policyResource: { apiGroup: string; kind: string };
  /** API client for fetching data */
  apiClient: ActivityApiClient;
  /** Additional CSS class */
  className?: string;
}

/**
 * RulePreviewPanel shows live preview of a rule against sample data.
 * Uses PolicyPreview API with auto-fetch to automatically load and test sample data.
 */
export function RulePreviewPanel({
  rule,
  ruleType,
  policyResource,
  apiClient,
  className = '',
}: RulePreviewPanelProps) {
  const [isLoading, setIsLoading] = useState(false);
  const [samples, setSamples] = useState<PolicyPreviewInput[]>([]);
  const [preview, setPreview] = useState<PolicyPreviewStatus | null>(null);
  const [error, setError] = useState<Error | null>(null);

  // Use ref to track the debounce timeout
  const debounceTimerRef = useRef<number | null>(null);

  // Re-run preview when rule changes (debounced)
  useEffect(() => {
    // Clear any existing timer
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }

    // Debounce the preview execution
    debounceTimerRef.current = setTimeout(() => {
      const runPreview = async () => {
        setIsLoading(true);
        setError(null);

        try {
          // Use auto-fetch to get sample data and run preview in one API call
          const result = await apiClient.createPolicyPreview({
            policy: {
              resource: policyResource,
              auditRules: ruleType === 'audit' ? [rule] : undefined,
              eventRules: ruleType === 'event' ? [rule] : undefined,
            },
            autoFetch: {
              limit: 10,
              timeRange: '7d',
              sources: ruleType === 'audit' ? 'audit' : 'events',
            },
          });

          setPreview(result.status || null);

          // Update samples from fetchedInputs so UI can display what was tested
          if (result.status?.fetchedInputs) {
            setSamples(result.status.fetchedInputs);
          }
        } catch (err) {
          setError(err instanceof Error ? err : new Error('Failed to run preview'));
        } finally {
          setIsLoading(false);
        }
      };

      runPreview();
    }, 500);

    // Cleanup
    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [rule, ruleType, policyResource, apiClient]);

  // Calculate statistics
  const stats = {
    total: samples.length,
    matched: preview?.results?.filter((r) => r.matched).length || 0,
    errors: preview?.results?.filter((r) => !!r.error).length || 0,
  };

  return (
    <div className={cn('space-y-4', className)}>
      {/* Loading State */}
      {isLoading && (
        <Card>
          <CardContent className="py-8 text-center">
            <Loader2 className="h-6 w-6 animate-spin mx-auto mb-2 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              Loading sample {ruleType === 'audit' ? 'audit logs' : 'events'} and testing rule...
            </p>
          </CardContent>
        </Card>
      )}

      {/* Error State */}
      {error && !isLoading && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      )}

      {/* Empty State - No samples found */}
      {!isLoading && samples.length === 0 && !error && (
        <Card>
          <CardContent className="py-12 text-center">
            <p className="text-sm text-muted-foreground mb-2">
              No sample data found
            </p>
            <p className="text-xs text-muted-foreground">
              No recent {ruleType === 'audit' ? 'audit logs' : 'events'} found for{' '}
              {policyResource.apiGroup}/{policyResource.kind}
            </p>
          </CardContent>
        </Card>
      )}

      {/* Preview Results */}
      {!isLoading && preview && samples.length > 0 && (
        <div className="space-y-4">
          {/* Summary Header */}
          <div className="flex items-center gap-3">
            <Badge variant={stats.matched > 0 ? 'success' : 'secondary'}>
              {stats.matched} matched
            </Badge>
            <span className="text-muted-foreground">/</span>
            <span className="text-sm text-muted-foreground">{stats.total} tested</span>
            {stats.errors > 0 && (
              <>
                <span className="text-muted-foreground">/</span>
                <Badge variant="destructive">{stats.errors} errors</Badge>
              </>
            )}
          </div>

          {/* Sample Results */}
          <Card>
            <CardContent className="p-0">
              <div className="divide-y divide-border">
                {preview.results?.map((result, index) => {
                  const input = samples[result.inputIndex];
                  const isMatched = result.matched;
                  const hasError = !!result.error;
                  const activity = preview.activities?.[
                    preview.activities.findIndex((_, idx) => idx === index)
                  ];

                  return (
                    <div
                      key={result.inputIndex}
                      className={cn(
                        'p-4 flex flex-col gap-3',
                        hasError && 'bg-destructive/10'
                      )}
                    >
                      {/* Sample Input */}
                      {input?.type === 'audit' && input.audit && (
                        <AuditLogFeedItem event={input.audit} compact={true} />
                      )}
                      {input?.type === 'event' && input.event && (
                        <EventFeedItem
                          event={input.event as K8sEvent}
                          compact={true}
                        />
                      )}

                      {/* Match Result */}
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          {hasError ? (
                            <Badge variant="destructive" className="gap-1">
                              <XCircle className="h-3 w-3" />
                              Error
                            </Badge>
                          ) : isMatched ? (
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
                        </div>

                        {/* Error Message */}
                        {hasError && (
                          <span className="text-xs text-destructive">{result.error}</span>
                        )}
                      </div>

                      {/* Generated Summary */}
                      {isMatched && activity && !hasError && (
                        <div className="mt-2 p-3 rounded-md bg-muted">
                          <p className="text-xs text-muted-foreground mb-1.5">
                            Generated Summary:
                          </p>
                          <ActivityFeedSummary
                            summary={activity.spec.summary}
                            links={activity.spec.links}
                          />
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </CardContent>
          </Card>

          {/* No Matches Warning */}
          {stats.matched === 0 && stats.errors === 0 && (
            <Alert>
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                This rule did not match any of the {stats.total} sample{stats.total !== 1 ? 's' : ''}.
                Check your match expression.
              </AlertDescription>
            </Alert>
          )}
        </div>
      )}
    </div>
  );
}
