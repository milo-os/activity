import type {
  PolicyPreviewStatus,
  PolicyPreviewInput,
} from '../types/policy';
import type { ResourceRef } from '../types/activity';
import { PolicyPreviewResult } from './PolicyPreviewResult';
import { cn } from '../lib/utils';
import { Card, CardContent } from '@datum-cloud/datum-ui/card';
import { Button } from '@datum-cloud/datum-ui/button';
import { Alert, AlertDescription } from '@datum-cloud/datum-ui/alert';
import { AlertCircle, Loader2 } from 'lucide-react';

export interface PolicyPreviewPanelProps {
  /** Preview result (if available) */
  result: PolicyPreviewStatus | null;
  /** Input data used for preview */
  inputs?: PolicyPreviewInput[];
  /** Whether preview is loading */
  isLoading: boolean;
  /** Error message */
  error: Error | null;
  /** Handler for resource link clicks in result */
  onResourceClick?: (resource: ResourceRef) => void;
  /** Callback to run preview (with auto-fetch) */
  onRunPreview?: () => void;
  /** Additional CSS class */
  className?: string;
}


/**
 * PolicyPreviewPanel displays preview results for testing policies
 */
export function PolicyPreviewPanel({
  result,
  inputs,
  isLoading,
  error,
  onResourceClick,
  onRunPreview,
  className = '',
}: PolicyPreviewPanelProps) {

  return (
    <div className={cn('space-y-3', className)}>
      {/* Header - only show when there's a refresh button */}
      {onRunPreview && (
        <div className="flex items-center justify-between">
          <p className="text-xs text-muted-foreground">
            Preview the activity timeline for recent resource changes.
          </p>
          <Button
            type="tertiary" theme="outline"
            size="small"
            onClick={onRunPreview}
            disabled={isLoading}
            className="h-7 text-xs"
          >
            {isLoading ? (
              <>
                <Loader2 className="h-3 w-3 mr-1 animate-spin" />
                Loading...
              </>
            ) : (
              'Refresh Preview'
            )}
          </Button>
        </div>
      )}

      {/* Loading Preview State - Skeleton */}
      {isLoading && (
        <div className="space-y-2">
          {/* Activity stream skeleton */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <div className="h-3 w-32 bg-muted rounded animate-pulse" />
              <div className="h-4 w-16 bg-muted rounded animate-pulse" />
            </div>
            <div className="space-y-0 border rounded-md divide-y">
              {[1, 2, 3].map((i) => (
                <div key={i} className="px-2 py-1.5 flex items-center gap-2">
                  <div className="h-5 w-5 bg-muted rounded-full animate-pulse shrink-0" />
                  <div className="flex-1 space-y-1">
                    <div className="h-2.5 w-3/4 bg-muted rounded animate-pulse" />
                    <div className="h-2 w-1/2 bg-muted rounded animate-pulse" />
                  </div>
                  <div className="h-2.5 w-14 bg-muted rounded animate-pulse shrink-0" />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Error Display */}
      {error && !result && !isLoading && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      )}

      {/* Empty State - No result yet and not loading */}
      {!result && !isLoading && !error && (
        <Card>
          <CardContent className="py-6 text-center">
            <p className="text-xs text-muted-foreground mb-1">
              No preview available yet
            </p>
            <p className="text-[11px] text-muted-foreground">
              Enter a match expression and summary template to see results.
            </p>
          </CardContent>
        </Card>
      )}

      {/* Preview Result */}
      {result && !isLoading && (
        <PolicyPreviewResult
          result={result}
          inputs={inputs}
          onResourceClick={onResourceClick}
        />
      )}
    </div>
  );
}
