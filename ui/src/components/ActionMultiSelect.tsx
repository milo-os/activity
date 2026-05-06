import * as React from 'react';
import * as Popover from '@radix-ui/react-popover';
import { ChevronDown } from 'lucide-react';
import { Checkbox } from '@datum-cloud/datum-ui/checkbox';
import { cn } from '../lib/utils';

export interface ActionMultiSelectOption {
  value: string;
  label: string;
  count?: number;
}

export interface ActionMultiSelectProps {
  /** Current selected verbs */
  value: string[];
  /** Handler called when selection changes */
  onChange: (verbs: string[]) => void;
  /** Additional CSS class */
  className?: string;
  /** Whether the select is disabled */
  disabled?: boolean;
  /** Available action options with counts */
  options: ActionMultiSelectOption[];
  /** Whether facets are still loading */
  isLoading?: boolean;
}

/**
 * ActionMultiSelect provides a compact multi-select dropdown for filtering by action/verb.
 * Uses checkboxes for multiple selection and displays counts from facet queries.
 */
export function ActionMultiSelect({
  value,
  onChange,
  className = '',
  disabled = false,
  options,
  isLoading = false,
}: ActionMultiSelectProps) {
  const [open, setOpen] = React.useState(false);

  const handleToggle = React.useCallback(
    (actionValue: string) => {
      if (value.includes(actionValue)) {
        onChange(value.filter((v) => v !== actionValue));
      } else {
        onChange([...value, actionValue]);
      }
    },
    [value, onChange]
  );

  const displayText = React.useMemo(() => {
    if (value.length === 0) return 'Actions';
    if (value.length === 1) return '1 action';
    return `${value.length} actions`;
  }, [value.length]);

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          disabled={disabled || isLoading}
          className={cn(
            'flex h-7 items-center justify-between gap-2 rounded-md border border-input bg-background px-2 text-xs min-w-[100px]',
            'hover:bg-accent hover:text-accent-foreground transition-colors',
            'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
            'disabled:cursor-not-allowed disabled:opacity-50',
            className
          )}
        >
          <span className="font-medium">{displayText}</span>
          <ChevronDown className="h-3 w-3 text-muted-foreground shrink-0" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className={cn(
            'z-50 min-w-[160px] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
            'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
            'data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2'
          )}
          sideOffset={4}
          align="start"
        >
          <div className="p-1">
            {isLoading ? (
              <div className="px-3 py-2 text-sm text-muted-foreground">Loading...</div>
            ) : options.length === 0 ? (
              <div className="px-3 py-2 text-sm text-muted-foreground">No actions found</div>
            ) : (
              options.map((option) => {
                const checked = value.includes(option.value);
                return (
                  <label
                    key={option.value}
                    className={cn(
                      'flex items-center gap-2 rounded-sm px-3 py-2 text-sm cursor-pointer',
                      'hover:bg-accent hover:text-accent-foreground transition-colors'
                    )}
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={() => handleToggle(option.value)}
                      className="h-4 w-4"
                    />
                    <span className="flex-1">{option.label}</span>
                    {option.count !== undefined && (
                      <span className="text-xs text-muted-foreground">({option.count})</span>
                    )}
                  </label>
                );
              })
            )}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
