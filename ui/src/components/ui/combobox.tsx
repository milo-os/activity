import * as React from 'react';
import { Autocomplete } from '@datum-cloud/datum-ui/autocomplete';

export interface ComboboxOption {
  value: string;
  label: string;
  count?: number;
}

export interface ComboboxProps {
  options: ComboboxOption[];
  value: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  loading?: boolean;
  className?: string;
  /** Allow clearing the selection */
  clearable?: boolean;
  /** Show "All" option at the top */
  showAllOption?: boolean;
  allOptionLabel?: string;
}

/** Sentinel for the "All" item; the public value stays '' for "no filter". */
const ALL_VALUE = '__all__';

export function Combobox({
  options,
  value,
  onValueChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyMessage = 'No results',
  disabled,
  loading,
  className,
  clearable = false,
  showAllOption = true,
  allOptionLabel = 'All',
}: ComboboxProps) {
  const items = React.useMemo<ComboboxOption[]>(() => {
    const base = options.map((o) => ({ value: o.value, label: o.label, count: o.count }));
    return showAllOption ? [{ value: ALL_VALUE, label: allOptionLabel }, ...base] : base;
  }, [options, showAllOption, allOptionLabel]);

  // An empty value selects the "All" item so the trigger reads its label.
  const selectedValue = value === '' && showAllOption ? ALL_VALUE : value;

  return (
    <Autocomplete
      className={className}
      options={items}
      value={selectedValue}
      onValueChange={(next) => onValueChange(next === ALL_VALUE ? '' : next)}
      placeholder={placeholder}
      searchPlaceholder={searchPlaceholder}
      emptyContent={emptyMessage}
      disabled={disabled}
      loading={loading}
      footer={
        clearable && value ? (
          <button
            type="button"
            className="text-muted-foreground hover:text-foreground w-full px-2 py-1.5 text-left text-xs"
            onClick={() => onValueChange('')}
          >
            Clear selection
          </button>
        ) : undefined
      }
      renderValue={(option) => (
        <span className="truncate">
          {option.count !== undefined ? `${option.label} (${option.count})` : option.label}
        </span>
      )}
      renderOption={(option) => (
        <span className="flex w-full items-center justify-between gap-2">
          <span className="truncate">{option.label}</span>
          {'count' in option && option.count !== undefined && (
            <span className="text-muted-foreground text-xs">{option.count}</span>
          )}
        </span>
      )}
    />
  );
}
