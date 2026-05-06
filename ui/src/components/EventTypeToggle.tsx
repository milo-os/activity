import { Button } from '@datum-cloud/datum-ui/button';
import { cn } from '../lib/utils';
import type { K8sEventType } from '../types/k8s-event';

export type EventTypeOption = K8sEventType | 'all';

export interface EventTypeToggleProps {
  /** Current selected value */
  value: EventTypeOption;
  /** Handler called when selection changes */
  onChange: (value: EventTypeOption) => void;
  /** Additional CSS class */
  className?: string;
  /** Whether the toggle is disabled */
  disabled?: boolean;
}

/**
 * Options for the event type toggle
 */
const OPTIONS: { value: EventTypeOption; label: string; description: string }[] = [
  {
    value: 'all',
    label: 'All',
    description: 'Show all events',
  },
  {
    value: 'Normal',
    label: 'Normal',
    description: 'Show only normal events',
  },
  {
    value: 'Warning',
    label: 'Warning',
    description: 'Show only warning events',
  },
];

/**
 * EventTypeToggle provides a segmented control for filtering by event type
 */
export function EventTypeToggle({
  value,
  onChange,
  className = '',
  disabled = false,
}: EventTypeToggleProps) {
  return (
    <div
      className={cn('inline-flex border border-input rounded-md overflow-hidden', className)}
      role="group"
      aria-label="Filter by event type"
    >
      {OPTIONS.map((option, index) => {
        const active = value === option.value;
        return (
          <Button
            key={option.value}
            htmlType="button"
            type={active ? 'primary' : 'quaternary'}
            theme={active ? 'solid' : 'borderless'}
            className={cn(
              'px-2 h-7 text-xs font-medium transition-all duration-200',
              index < OPTIONS.length - 1 && 'border-r border-input',
              !active && 'bg-muted text-foreground hover:bg-muted/80'
            )}
            style={active ? { borderRadius: 0, border: 0 } : { borderRadius: 0 }}
            onClick={() => onChange(option.value)}
            disabled={disabled}
            aria-pressed={active}
            title={option.description}
          >
            {option.label}
          </Button>
        );
      })}
    </div>
  );
}
