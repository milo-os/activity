import type { K8sEventType } from '../types/k8s-event';
import { SegmentedToggle, type SegmentedToggleOption } from './ui/segmented-toggle';

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
const OPTIONS: SegmentedToggleOption<EventTypeOption>[] = [
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
  className,
  disabled = false,
}: EventTypeToggleProps) {
  return (
    <SegmentedToggle
      options={OPTIONS}
      value={value}
      onChange={onChange}
      ariaLabel="Filter by event type"
      className={className}
      disabled={disabled}
    />
  );
}
