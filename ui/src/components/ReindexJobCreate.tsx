import { useState } from 'react';
import type { ReindexJobSpec } from '../types/reindex';
import type { ErrorFormatter } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardHeader, CardContent } from '@datum-cloud/datum-ui/card';
import { ApiErrorAlert } from './ApiErrorAlert';
import { Loader2, RefreshCw } from 'lucide-react';

export interface ReindexJobCreateProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Pre-selected policy name (required for simplified flow) */
  policyName?: string;
  /** Callback when job is successfully created */
  onCreate?: (jobName: string) => void;
  /** Callback when cancel is clicked */
  onCancel?: () => void;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

const TIME_RANGE_OPTIONS = [
  { label: 'Last hour', value: 'now-1h' },
  { label: 'Last 24 hours', value: 'now-24h' },
  { label: 'Last 7 days', value: 'now-7d' },
  { label: 'Last 30 days', value: 'now-30d' },
  { label: 'All time', value: 'now-365d' },
] as const;

/**
 * Simplified ReindexJobCreate for quick policy reindexing
 */
export function ReindexJobCreate({
  client,
  policyName,
  onCreate,
  onCancel,
  className = '',
  errorFormatter,
}: ReindexJobCreateProps) {
  const [selectedRange, setSelectedRange] = useState('now-7d');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);
    setError(null);

    try {
      // Generate job name from policy and timestamp
      const timestamp = Date.now().toString(36);
      const baseName = policyName
        ? `reindex-${policyName}-${timestamp}`
        : `reindex-${timestamp}`;
      const jobName = baseName.toLowerCase().replace(/[^a-z0-9-]/g, '-').slice(0, 63);

      const spec: ReindexJobSpec = {
        timeRange: {
          startTime: selectedRange,
          endTime: 'now',
        },
        // Only set policy selector if a specific policy is provided
        ...(policyName && {
          policySelector: {
            names: [policyName],
          },
        }),
      };

      const job = await client.createReindexJob(jobName, spec);
      onCreate?.(job.metadata?.name || jobName);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to create job'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const selectedOption = TIME_RANGE_OPTIONS.find(o => o.value === selectedRange);

  return (
    <Card className={`rounded-xl max-w-md ${className}`}>
      <CardHeader className="p-6 border-b border-border">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-primary/10 rounded-lg">
            <RefreshCw className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-foreground m-0">
              {policyName ? `Reindex "${policyName}"` : 'Reindex Activities'}
            </h2>
            <p className="text-sm text-muted-foreground m-0">
              Re-process events with current policy rules
            </p>
          </div>
        </div>
      </CardHeader>

      <CardContent className="p-6">
        <ApiErrorAlert error={error} className="mb-4" errorFormatter={errorFormatter} />

        <form onSubmit={handleSubmit} className="space-y-6">
          {/* Time Range Selection */}
          <div>
            <label className="block text-sm font-medium text-foreground mb-3">
              Time range to reindex
            </label>
            <div className="grid grid-cols-1 gap-2">
              {TIME_RANGE_OPTIONS.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => setSelectedRange(option.value)}
                  className={`px-4 py-3 text-left rounded-lg border transition-all ${
                    selectedRange === option.value
                      ? 'border-primary bg-primary/5 text-foreground'
                      : 'border-input bg-background text-muted-foreground hover:border-primary/50 hover:bg-muted/50'
                  }`}
                >
                  <span className="font-medium">{option.label}</span>
                </button>
              ))}
            </div>
          </div>

          {/* Summary */}
          <div className="p-4 bg-muted/50 rounded-lg text-sm text-muted-foreground">
            {policyName ? (
              <p className="m-0">
                This will re-process all events from <strong className="text-foreground">{selectedOption?.label.toLowerCase()}</strong> using
                the current rules in <strong className="text-foreground">{policyName}</strong>.
              </p>
            ) : (
              <p className="m-0">
                This will re-process all events from <strong className="text-foreground">{selectedOption?.label.toLowerCase()}</strong> using
                all current policy rules.
              </p>
            )}
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2">
            {onCancel && (
              <Button
                htmlType="button"
                type="tertiary" theme="outline"
                onClick={onCancel}
                disabled={isSubmitting}
              >
                Cancel
              </Button>
            )}
            <Button
              htmlType="submit"
              disabled={isSubmitting}
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Starting...
                </>
              ) : (
                'Start Reindex'
              )}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
