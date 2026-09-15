import * as React from 'react';
import { Popover, PopoverTrigger, PopoverContent } from '@datum-cloud/datum-ui/popover';
import { ChevronDown } from 'lucide-react';
import { Checkbox } from '@datum-cloud/datum-ui/checkbox';
import { Button } from '@datum-cloud/datum-ui/button';
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
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          htmlType="button"
          type="quaternary"
          theme="outline"
          size="small"
          disabled={disabled || isLoading}
          className={cn('min-w-[100px] justify-between gap-2', className)}
        >
          <span className="font-medium">{displayText}</span>
          <ChevronDown className="h-3 w-3 text-muted-foreground shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto min-w-[160px] p-0" sideOffset={4} align="start">
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
        </PopoverContent>
    </Popover>
  );
}
