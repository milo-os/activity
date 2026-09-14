import { Button } from '@datum-cloud/datum-ui/button';
import { ButtonGroup } from '@datum-cloud/datum-ui/button-group';
import { cn } from '../../lib/utils';

export interface SegmentedToggleOption<T extends string> {
  value: T;
  label: string;
  description: string;
}

export interface SegmentedToggleProps<T extends string> {
  /** Segments to render, in order */
  options: readonly SegmentedToggleOption<T>[];
  /** Current selected value */
  value: T;
  /** Handler called when selection changes */
  onChange: (value: T) => void;
  /** Accessible name for the group */
  ariaLabel: string;
  /** Additional CSS class */
  className?: string;
  /** Whether the toggle is disabled */
  disabled?: boolean;
}

/**
 * SegmentedToggle - a single-select control built on datum-ui's ButtonGroup.
 * The active segment is a solid primary Button; inactive segments sit on the
 * neutral (muted) button surface so the control reads in both themes.
 */
export function SegmentedToggle<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
  className,
  disabled = false,
}: SegmentedToggleProps<T>) {
  return (
    <ButtonGroup aria-label={ariaLabel} className={className}>
      {options.map((option) => {
        const active = value === option.value;
        return (
          <Button
            key={option.value}
            htmlType="button"
            size="small"
            type={active ? 'primary' : 'quaternary'}
            theme={active ? 'solid' : 'light'}
            className={cn('font-medium', !active && 'border border-border')}
            onClick={() => onChange(option.value)}
            disabled={disabled}
            aria-pressed={active}
            title={option.description}
          >
            {option.label}
          </Button>
        );
      })}
    </ButtonGroup>
  );
}
