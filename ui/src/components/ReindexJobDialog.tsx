import { useState } from 'react';
import type { ReindexJobSpec } from '../types/reindex';
import type { ErrorFormatter } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { Button } from '@datum-cloud/datum-ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from './ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@datum-cloud/datum-ui/select';
import { ApiErrorAlert } from './ApiErrorAlert';
import { Loader2, RefreshCw } from 'lucide-react';

export interface ReindexJobDialogProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Policy name to reindex */
  policyName: string;
  /** Whether the dialog is open */
  open: boolean;
  /** Callback when dialog should close */
  onOpenChange: (open: boolean) => void;
  /** Callback when job is successfully created */
  onSuccess?: (jobName: string) => void;
  /** Custom error formatter */
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
 * Dialog for quickly reindexing a policy
 */
export function ReindexJobDialog({
  client,
  policyName,
  open,
  onOpenChange,
  onSuccess,
  errorFormatter,
}: ReindexJobDialogProps) {
  const [selectedRange, setSelectedRange] = useState('now-7d');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const handleSubmit = async () => {
    setIsSubmitting(true);
    setError(null);

    try {
      const timestamp = Date.now().toString(36);
      const jobName = `reindex-${policyName}-${timestamp}`
        .toLowerCase()
        .replace(/[^a-z0-9-]/g, '-')
        .slice(0, 63);

      const spec: ReindexJobSpec = {
        timeRange: {
          startTime: selectedRange,
          endTime: 'now',
        },
        policySelector: {
          names: [policyName],
        },
      };

      const job = await client.createReindexJob(jobName, spec);
      onOpenChange(false);
      onSuccess?.(job.metadata?.name || jobName);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to create job'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const selectedOption = TIME_RANGE_OPTIONS.find(o => o.value === selectedRange);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="p-2 bg-primary/10 rounded-lg">
              <RefreshCw className="h-5 w-5 text-primary" />
            </div>
            <div>
              <DialogTitle>Reindex Policy</DialogTitle>
              <DialogDescription className="text-xs">
                Re-process events for &quot;{policyName}&quot;
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <ApiErrorAlert error={error} errorFormatter={errorFormatter} />

          <div>
            <label className="block text-sm font-medium text-foreground mb-2">
              Time range
            </label>
            <Select value={selectedRange} onValueChange={setSelectedRange}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Select time range" />
              </SelectTrigger>
              <SelectContent>
                {TIME_RANGE_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <p className="text-sm text-muted-foreground">
            Events from {selectedOption?.label.toLowerCase()} will be re-processed.
          </p>
        </div>

        <div className="flex justify-end gap-2">
          <Button
            htmlType="button"
            type="tertiary" theme="outline"
            onClick={() => onOpenChange(false)}
            disabled={isSubmitting}
          >
            Cancel
          </Button>
          <Button
            htmlType="button"
            onClick={handleSubmit}
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
      </DialogContent>
    </Dialog>
  );
}
