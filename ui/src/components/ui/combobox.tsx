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

const ALL_VALUE = '';

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
  clearable,
  showAllOption,
  allOptionLabel = 'All',
}: ComboboxProps) {
  const items = React.useMemo(() => {
    const base = options.map((o) => ({ value: o.value, label: o.label, count: o.count }));
    return showAllOption ? [{ value: ALL_VALUE, label: allOptionLabel }, ...base] : base;
  }, [options, showAllOption, allOptionLabel]);

  return (
    <Autocomplete
      className={className}
      options={items}
      value={value}
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
