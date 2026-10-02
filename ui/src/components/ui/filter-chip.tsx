import * as React from 'react';
import { X, ChevronDown } from 'lucide-react';
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from '@datum-cloud/datum-ui/command';
import { Popover, PopoverTrigger, PopoverContent } from '@datum-cloud/datum-ui/popover';
import { Button } from '@datum-cloud/datum-ui/button';
import { ButtonGroup } from '@datum-cloud/datum-ui/button-group';
import { cn } from '../../lib/utils';
import { Input } from '@datum-cloud/datum-ui/input';

export interface FilterChipOption {
  value: string;
  label: string;
  count?: number;
}

export interface FilterChipProps {
  /** Label for the filter (e.g., "Kind", "Reason") */
  label: string;
  /** Selected values */
  values: string[];
  /** Available options for multi-select mode */
  options?: FilterChipOption[];
  /** Handler for value changes */
  onValuesChange: (values: string[]) => void;
  /** Handler for clearing the filter */
  onClear: () => void;
  /** Input mode - typeahead for multi-select, text for text input */
  inputMode?: 'typeahead' | 'text';
  /** Auto-open popover when filter is first added */
  autoOpen?: boolean;
  /** Called when popover closes (useful for cleanup when no value selected) */
  onPopoverClose?: () => void;
  /** Placeholder for text input mode */
  placeholder?: string;
  /** Search placeholder for typeahead mode */
  searchPlaceholder?: string;
  /** Whether the chip is disabled */
  disabled?: boolean;
  /** Additional class name */
  className?: string;
}

/**
 * FilterChip - A compact pill/chip showing "Label: Values" with clear button
 * Clicking opens a popover for editing the filter values
 */
export function FilterChip({
  label,
  values,
  options = [],
  onValuesChange,
  onClear,
  inputMode = 'typeahead',
  autoOpen = false,
  onPopoverClose,
  placeholder = 'Enter value...',
  searchPlaceholder = 'Search...',
  disabled = false,
  className,
}: FilterChipProps) {
  const [open, setOpen] = React.useState(false);
  const hasAutoOpenedRef = React.useRef(false);
  const wasEverOpenRef = React.useRef(false);

  // When autoOpen becomes true, open the popover
  React.useEffect(() => {
    if (autoOpen && !hasAutoOpenedRef.current) {
      // Delay to ensure the AddFilterDropdown's popover has fully closed
      // before opening this one. This prevents the new popover from being
      // immediately closed by residual click/focus events from the dropdown.
      hasAutoOpenedRef.current = true;
      requestAnimationFrame(() => {
        setTimeout(() => {
          setOpen(true);
          wasEverOpenRef.current = true;
        }, 500);
      });
    }
  }, [autoOpen]);

  // Handle popover open state changes
  const handleOpenChange = React.useCallback(
    (newOpen: boolean) => {
      setOpen(newOpen);
      if (newOpen) {
        wasEverOpenRef.current = true;
      }
      // Only call onPopoverClose if the popover was actually opened at some point
      if (!newOpen && onPopoverClose && wasEverOpenRef.current) {
        onPopoverClose();
      }
    },
    [onPopoverClose]
  );
  const [search, setSearch] = React.useState('');
  const [textValue, setTextValue] = React.useState(values[0] || '');

  // Update text value when values change externally (for text input mode)
  React.useEffect(() => {
    if (inputMode === 'text' && values.length > 0) {
      setTextValue(values[0]);
    }
  }, [values, inputMode]);

  // Get display value for the chip
  const displayValue = React.useMemo(() => {
    if (inputMode === 'text') {
      return values[0] || '';
    }

    if (values.length === 0) return '';
    if (values.length === 1) return values[0];
    if (values.length === 2) return `${values[0]}, ${values[1]}`;
    return `${values.length} selected`;
  }, [values, inputMode]);

  // Handle selection in typeahead mode
  const handleSelect = React.useCallback(
    (selectedValue: string) => {
      if (values.includes(selectedValue)) {
        // Remove from selection
        const newValues = values.filter((v) => v !== selectedValue);
        onValuesChange(newValues);
        // If no values left, close the popover
        if (newValues.length === 0) {
          setOpen(false);
        }
      } else {
        // Add to selection
        onValuesChange([...values, selectedValue]);
      }
    },
    [onValuesChange, values]
  );

  // Handle remove individual value
  const handleRemoveValue = React.useCallback(
    (e: React.MouseEvent, valueToRemove: string) => {
      e.stopPropagation();
      const newValues = values.filter((v) => v !== valueToRemove);
      onValuesChange(newValues);
      // If no values left, close and clear
      if (newValues.length === 0) {
        setOpen(false);
      }
    },
    [onValuesChange, values]
  );

  // Handle clear all
  const handleClearAll = React.useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onClear();
      setOpen(false);
    },
    [onClear]
  );

  // Handle text input change
  const handleTextChange = React.useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const value = e.target.value;
      setTextValue(value);
      onValuesChange(value ? [value] : []);
    },
    [onValuesChange]
  );

  // Custom fuzzy filter function for typeahead
  const filterOptions = React.useCallback(
    (optionValue: string, searchQuery: string): number => {
      if (!searchQuery) return 1;

      const option = options.find((o) => o.value === optionValue);
      const optionLabel = option?.label || optionValue;
      const lowerLabel = optionLabel.toLowerCase();
      const lowerSearch = searchQuery.toLowerCase();

      // Exact match
      if (lowerLabel === lowerSearch) return 1;

      // Starts with
      if (lowerLabel.startsWith(lowerSearch)) return 0.9;

      // Contains
      if (lowerLabel.includes(lowerSearch)) return 0.8;

      // Fuzzy match - check if all search chars appear in order
      let searchIdx = 0;
      for (let i = 0; i < lowerLabel.length && searchIdx < lowerSearch.length; i++) {
        if (lowerLabel[i] === lowerSearch[searchIdx]) {
          searchIdx++;
        }
      }
      if (searchIdx === lowerSearch.length) return 0.6;

      return 0;
    },
    [options]
  );

  // Find selected options for chips display
  const selectedOptions = options.filter((opt) => values.includes(opt.value));

  return (
    <ButtonGroup aria-label={`${label} filter`} className={className}>
      <Popover open={open} onOpenChange={handleOpenChange}>
        <PopoverTrigger asChild>
          <Button
            htmlType="button"
            type="quaternary"
            theme="outline"
            size="small"
            disabled={disabled}
            className="gap-2 font-normal data-[state=open]:bg-btn-neutral-bg"
          >
            <span className="font-medium">{label}:</span>
            <span className="truncate max-w-[120px]">{displayValue}</span>
            <ChevronDown className="h-3 w-3 text-muted-foreground" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          className="w-auto min-w-[var(--radix-popover-trigger-width)] max-w-[320px] p-0"
          sideOffset={4}
          align="start"
        >
          {inputMode === 'typeahead' ? (
            <Command filter={filterOptions}>
              <CommandInput
                placeholder={searchPlaceholder}
                value={search}
                onValueChange={setSearch}
              />
              {/* Selected items chips */}
              {values.length > 0 && (
                <div className="flex flex-wrap gap-1 p-2 border-b">
                  {selectedOptions.map((option) => (
                    <span
                      key={option.value}
                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-accent text-accent-foreground text-xs"
                    >
                      {option.label}
                      <button
                        type="button"
                        onClick={(e) => handleRemoveValue(e, option.value)}
                        className="rounded-sm hover:bg-accent-foreground/20"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </span>
                  ))}
                </div>
              )}
              <CommandList>
                <CommandEmpty>No results found.</CommandEmpty>
                <CommandGroup>
                  {options.map((option) => (
                    <CommandItem
                      key={option.value}
                      value={option.value}
                      onSelect={() => handleSelect(option.value)}
                    >
                      <div className={cn(
                        'mr-2 h-4 w-4 shrink-0 rounded-sm border border-border',
                        values.includes(option.value) && 'bg-primary border-primary'
                      )}>
                        {values.includes(option.value) && (
                          <svg viewBox="0 0 12 12" fill="none" className="h-full w-full p-0.5 text-primary-foreground">
                            <path d="M2 6l3 3 5-5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                        )}
                      </div>
                      <span className="flex-1 truncate">{option.label}</span>
                      {option.count !== undefined && (
                        <span className="ml-2 text-xs text-muted-foreground">
                          ({option.count})
                        </span>
                      )}
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          ) : (
            <div className="p-3">
              <Input
                type="text"
                value={textValue}
                onChange={handleTextChange}
                placeholder={placeholder}
                className="w-full"
                autoFocus
              />
            </div>
          )}
        </PopoverContent>
      </Popover>
      <Button
        htmlType="button"
        type="quaternary"
        theme="outline"
        size="small"
        onClick={handleClearAll}
        disabled={disabled}
        aria-label={`Clear ${label} filter`}
      >
        <X className="h-3 w-3 text-muted-foreground" />
      </Button>
    </ButtonGroup>
  );
}
