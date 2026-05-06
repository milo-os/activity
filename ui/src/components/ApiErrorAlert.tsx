import { AlertCircle, AlertTriangle, RotateCw } from 'lucide-react';
import { Alert, AlertDescription } from '@datum-cloud/datum-ui/alert';
import { Button } from '@datum-cloud/datum-ui/button';
import { defaultErrorFormatter } from '../lib/errors';
import type { ErrorFormatter } from '../types/activity';

export interface ApiErrorAlertProps {
  error: Error | null;
  onRetry?: () => void;
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

type FriendlyError = {
  friendlyTitle: string;
  friendlyMessage: string;
  suggestion?: string | null;
  severity: 'warning' | 'error';
};

function isFriendlyError(error: Error): error is Error & FriendlyError {
  return 'friendlyTitle' in error && 'friendlyMessage' in error && 'severity' in error;
}

export function ApiErrorAlert({ error, onRetry, className, errorFormatter }: ApiErrorAlertProps) {
  if (!error) return null;

  // Use custom formatter if provided, otherwise use default
  const formatter = errorFormatter || defaultErrorFormatter;
  const formatted = formatter(error);

  // Determine error details
  const isFriendly = isFriendlyError(error);
  const message = formatted.message;
  const severity = isFriendly ? error.severity : 'error';

  // Choose alert variant and icon based on severity
  const alertVariant = severity === 'warning' ? 'warning' : 'destructive';
  const Icon = severity === 'warning' ? AlertTriangle : AlertCircle;

  return (
    <Alert variant={alertVariant} className={`py-2 px-3 [&>svg]:top-2.5 [&>svg]:left-3 ${className || ''}`}>
      <Icon className="h-4 w-4" />
      <AlertDescription className="flex items-center gap-2">
        <span className="text-sm flex-1 min-w-0 leading-tight">{message}</span>
        {onRetry && (
          <Button
            type="quaternary" theme="borderless"
            size="small"
            onClick={onRetry}
            className="shrink-0 h-6 w-6 p-0 -my-1"
            title="Retry"
            aria-label="Retry"
          >
            <RotateCw className="h-4 w-4" />
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}
