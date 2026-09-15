import { Button } from '@datum-cloud/datum-ui/button';
import { cn } from '../lib/utils';

export type VerbOption = 'all' | 'create' | 'update' | 'patch' | 'delete' | 'get' | 'list' | 'watch';

export interface VerbToggleProps {
  /** Current selected value */
  value: VerbOption;
  /** Handler called when selection changes */
  onChange: (value: VerbOption) => void;
  /** Additional CSS class */
  className?: string;
  /** Whether the toggle is disabled */
  disabled?: boolean;
}

/**
 * Options for the verb toggle - focusing on most common audit actions
 */
const OPTIONS: { value: VerbOption; label: string; description: string }[] = [
  {
    value: 'all',
    label: 'All',
    description: 'Show all actions',
  },
  {
    value: 'create',
    label: 'Create',
    description: 'Show only create actions',
  },
  {
    value: 'update',
    label: 'Update',
    description: 'Show only update actions',
  },
  {
    value: 'patch',
    label: 'Patch',
    description: 'Show only patch actions',
  },
  {
    value: 'delete',
    label: 'Delete',
    description: 'Show only delete actions',
  },
];

/**
 * VerbToggle provides a segmented control for filtering by audit action/verb
 */
export function VerbToggle({
  value,
  onChange,
  className = '',
  disabled = false,
}: VerbToggleProps) {
  return (
    <div
      className={cn('inline-flex border border-input rounded-md overflow-hidden', className)}
      role="group"
      aria-label="Filter by action"
    >
      {OPTIONS.map((option, index) => (
        <Button
          key={option.value}
          htmlType="button"
          type="quaternary"
          theme="borderless"
          className={cn(
            'rounded-none px-2 h-7 text-xs font-medium transition-all duration-200',
            index < OPTIONS.length - 1 && 'border-r border-input',
            value === option.value
              ? 'bg-[#BF9595] text-[#0C1D31] hover:bg-[#BF9595]/90'
              : 'bg-muted text-foreground hover:bg-muted/80'
          )}
          onClick={() => onChange(option.value)}
          disabled={disabled}
          aria-pressed={value === option.value}
          title={option.description}
        >
          {option.label}
        </Button>
      ))}
    </div>
  );
}
