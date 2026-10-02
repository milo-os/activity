import * as React from 'react';
import { Plus } from 'lucide-react';
import { Popover, PopoverTrigger, PopoverContent } from '@datum-cloud/datum-ui/popover';
import { Button } from '@datum-cloud/datum-ui/button';
import { cn } from '../../lib/utils';

export interface FilterOption {
  id: string;
  label: string;
  icon?: React.ReactNode;
}

export interface AddFilterDropdownProps {
  /** Available filter types that can be added */
  availableFilters: FilterOption[];
  /** IDs of filters that are already active */
  activeFilterIds: string[];
  /** Handler called when a filter is selected */
  onAddFilter: (filterId: string) => void;
  /** Whether any filters are currently active */
  hasActiveFilters?: boolean;
  /** Whether the dropdown is disabled */
  disabled?: boolean;
  /** Additional class name */
  className?: string;
}

/**
 * AddFilterDropdown - Shows "+ Add Filters" button that opens a dropdown of available filters
 * Already-active filters are dimmed/disabled in the list
 */
export function AddFilterDropdown({
  availableFilters,
  activeFilterIds,
  onAddFilter,
  hasActiveFilters = false,
  disabled = false,
  className,
}: AddFilterDropdownProps) {
  const [open, setOpen] = React.useState(false);

  const handleFilterClick = React.useCallback(
    (filterId: string) => {
      onAddFilter(filterId);
      setOpen(false);
    },
    [onAddFilter]
  );

  // Determine if a filter is active
  const isFilterActive = (filterId: string) => activeFilterIds.includes(filterId);

  // Get button label
  const buttonLabel = hasActiveFilters ? 'Filters' : 'Add Filters';

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          htmlType="button"
          type="quaternary"
          theme="outline"
          size="small"
          disabled={disabled}
          className={cn('gap-1.5 border-dashed text-muted-foreground hover:text-foreground', className)}
        >
          <Plus className="h-4 w-4" />
          <span className="font-medium">{buttonLabel}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto min-w-[180px] p-0" sideOffset={4} align="start">
          <div className="p-1">
            {availableFilters.map((filter) => {
              const active = isFilterActive(filter.id);
              return (
                <button
                  key={filter.id}
                  type="button"
                  onClick={() => !active && handleFilterClick(filter.id)}
                  disabled={active}
                  className={cn(
                    'relative flex w-full items-center gap-2 rounded-sm px-3 py-2 text-sm outline-none transition-colors',
                    active
                      ? 'cursor-not-allowed opacity-40'
                      : 'cursor-pointer hover:bg-accent hover:text-accent-foreground'
                  )}
                >
                  {filter.icon && <span className="text-muted-foreground">{filter.icon}</span>}
                  <span>{filter.label}</span>
                  {active && (
                    <span className="ml-auto text-xs text-muted-foreground">(active)</span>
                  )}
                </button>
              );
            })}
          </div>
      </PopoverContent>
    </Popover>
  );
}
