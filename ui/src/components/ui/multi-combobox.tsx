import * as React from 'react';
import { MultiSelect } from '@datum-cloud/datum-ui/multi-select';

export interface MultiComboboxOption {
  value: string;
  label: string;
  count?: number;
}

export interface MultiComboboxProps {
  options: MultiComboboxOption[];
  values: string[];
  onValuesChange: (values: string[]) => void;
  placeholder?: string;
  /** Kept for API compatibility; MultiSelect has no search placeholder prop. */
  searchPlaceholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  loading?: boolean;
  className?: string;
  /** Maximum number of selections to show in the trigger before collapsing */
  maxDisplayed?: number;
}

export function MultiCombobox({
  options,
  values,
  onValuesChange,
  placeholder = 'Select…',
  // searchPlaceholder is accepted for API compatibility but MultiSelect has no counterpart.
  emptyMessage = 'No results',
  disabled,
  loading,
  className,
  maxDisplayed = 3,
}: MultiComboboxProps) {
  const items = React.useMemo(
    () => options.map((o) => ({ value: o.value, label: o.label })),
    [options]
  );
  return (
    <MultiSelect
      className={className}
      options={items}
      value={values}
      onValueChange={onValuesChange}
      placeholder={placeholder}
      emptyContent={emptyMessage}
      isLoading={loading}
      disabled={disabled}
      maxCount={maxDisplayed}
    />
  );
}
