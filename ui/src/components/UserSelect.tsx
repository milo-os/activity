import * as React from 'react';
import { User, X } from 'lucide-react';
import * as Popover from '@radix-ui/react-popover';
import { Button } from '@datum-cloud/datum-ui/button';
import { cn } from '../lib/utils';

export interface UserOption {
  value: string;
  label: string;
  count?: number;
}

export interface UserSelectProps {
  /** Currently selected user (undefined = All) */
  value?: string;
  /** Available user options from facets */
  options: UserOption[];
  /** Handler called when selection changes */
  onChange: (value?: string) => void;
  /** Additional CSS class */
  className?: string;
  /** Whether the select is disabled */
  disabled?: boolean;
  /** Whether to show loading state */
  isLoading?: boolean;
}

/**
 * UserSelect provides a dropdown for filtering by user with facet counts
 */
export function UserSelect({
  value,
  options,
  onChange,
  className = '',
  disabled = false,
  isLoading = false,
}: UserSelectProps) {
  const [open, setOpen] = React.useState(false);
  const [search, setSearch] = React.useState('');

  // Filter options by search query
  const filteredOptions = React.useMemo(() => {
    if (!search) return options;
    const searchLower = search.toLowerCase();
    return options.filter((opt) => opt.label.toLowerCase().includes(searchLower));
  }, [options, search]);

  // Get display label
  const displayLabel = React.useMemo(() => {
    if (!value) return 'All Users';
    const selected = options.find((opt) => opt.value === value);
    return selected ? selected.label : value;
  }, [value, options]);

  const handleSelect = React.useCallback(
    (selectedValue?: string) => {
      onChange(selectedValue);
      setOpen(false);
      setSearch('');
    },
    [onChange]
  );

  const handleClear = React.useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onChange(undefined);
    },
    [onChange]
  );

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <Button
          htmlType="button"
          type="tertiary" theme="outline"
          size="small"
          disabled={disabled}
          className={cn(
            'h-7 text-xs font-medium gap-1.5 px-2',
            value && 'pr-1',
            className
          )}
        >
          <User className="h-3.5 w-3.5" />
          <span>{displayLabel}</span>
          {value && (
            <button
              onClick={handleClear}
              className="ml-1 rounded-sm opacity-70 hover:opacity-100 hover:bg-muted p-0.5"
              aria-label="Clear user filter"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </Button>
      </Popover.Trigger>

      <Popover.Portal>
        <Popover.Content
          className="z-50 w-64 rounded-md border bg-popover p-0 text-popover-foreground shadow-md outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2"
          align="start"
          sideOffset={4}
        >
          {/* Search Input */}
          <div className="border-b p-2">
            <input
              type="text"
              placeholder="Search users..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-0"
              autoFocus
            />
          </div>

          {/* Options List */}
          <div className="max-h-64 overflow-y-auto p-1">
            {/* All Users Option */}
            <button
              onClick={() => handleSelect(undefined)}
              className={cn(
                'w-full flex items-center justify-between rounded-sm px-2 py-1.5 text-xs cursor-pointer hover:bg-accent hover:text-accent-foreground',
                !value && 'bg-accent text-accent-foreground'
              )}
            >
              <span className="font-medium">All Users</span>
              {options.length > 0 && (
                <span className="text-muted-foreground">
                  {options.reduce((sum, opt) => sum + (opt.count || 0), 0)}
                </span>
              )}
            </button>

            {/* Loading State */}
            {isLoading && (
              <div className="px-2 py-4 text-center text-xs text-muted-foreground">
                Loading users...
              </div>
            )}

            {/* User Options */}
            {!isLoading && filteredOptions.length > 0 && (
              <div className="mt-1 border-t pt-1">
                {filteredOptions.map((option) => (
                  <button
                    key={option.value}
                    onClick={() => handleSelect(option.value)}
                    className={cn(
                      'w-full flex items-center justify-between rounded-sm px-2 py-1.5 text-xs cursor-pointer hover:bg-accent hover:text-accent-foreground',
                      value === option.value && 'bg-accent text-accent-foreground'
                    )}
                  >
                    <span className="truncate font-medium">{option.label}</span>
                    {option.count !== undefined && (
                      <span className="text-muted-foreground ml-2 flex-shrink-0">
                        {option.count}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            )}

            {/* Empty State */}
            {!isLoading && filteredOptions.length === 0 && search && (
              <div className="px-2 py-4 text-center text-xs text-muted-foreground">
                No users found
              </div>
            )}

            {!isLoading && options.length === 0 && !search && (
              <div className="px-2 py-4 text-center text-xs text-muted-foreground">
                No users available
              </div>
            )}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
