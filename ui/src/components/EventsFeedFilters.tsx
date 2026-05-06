import { useState, useCallback, useEffect } from 'react';
import { formatISO, subDays } from 'date-fns';
import { Search, X } from 'lucide-react';

import type { EventsFeedFilters as FilterState } from '../hooks/useEventsFeed';
import type { TimeRange } from '../hooks/useEventsFeed';
import type { ActivityApiClient } from '../api/client';
import { useEventFacets } from '../hooks/useEventFacets';
import { EventTypeToggle, EventTypeOption } from './EventTypeToggle';
import { TimeRangeDropdown } from './ui/time-range-dropdown';
import { FilterChip } from './ui/filter-chip';
import { AddFilterDropdown, type FilterOption } from './ui/add-filter-dropdown';
import { Input } from '@datum-cloud/datum-ui/input';

export interface EventsFeedFiltersProps {
  /** API client instance for fetching facets */
  client: ActivityApiClient;
  /** Current filter state */
  filters: FilterState;
  /** Current time range */
  timeRange: TimeRange;
  /** Handler called when filters change */
  onFiltersChange: (filters: FilterState) => void;
  /** Handler called when time range changes */
  onTimeRangeChange: (timeRange: TimeRange) => void;
  /** Whether the filters are disabled (e.g., during loading) */
  disabled?: boolean;
  /** Filters that should be locked and hidden from the UI (programmatically set by parent) */
  hiddenFilters?: Array<'involvedKinds' | 'reasons' | 'namespaces' | 'sourceComponents' | 'involvedName' | 'eventType'>;
  /** Additional CSS class */
  className?: string;
  /** Namespace filter (when scoped to a specific namespace) */
  namespace?: string;
}

/**
 * Preset time ranges
 */
const TIME_PRESETS = [
  { key: 'now-1h', label: 'Last hour' },
  { key: 'now-6h', label: 'Last 6 hours' },
  { key: 'now-24h', label: 'Last 24 hours' },
  { key: 'now-7d', label: 'Last 7 days' },
  { key: 'now-30d', label: 'Last 30 days' },
];

/**
 * Filter configuration registry
 */
type FilterId = 'involvedKinds' | 'reasons' | 'namespaces' | 'sourceComponents' | 'involvedName';

interface FilterConfig {
  id: FilterId;
  label: string;
  inputMode: 'typeahead' | 'text';
  placeholder?: string;
  searchPlaceholder?: string;
}

const FILTER_CONFIGS: Record<FilterId, FilterConfig> = {
  involvedKinds: {
    id: 'involvedKinds',
    label: 'Kind',
    inputMode: 'typeahead',
    searchPlaceholder: 'Search kinds...',
  },
  reasons: {
    id: 'reasons',
    label: 'Reason',
    inputMode: 'typeahead',
    searchPlaceholder: 'Search reasons...',
  },
  namespaces: {
    id: 'namespaces',
    label: 'Namespace',
    inputMode: 'typeahead',
    searchPlaceholder: 'Search namespaces...',
  },
  sourceComponents: {
    id: 'sourceComponents',
    label: 'Source',
    inputMode: 'typeahead',
    searchPlaceholder: 'Search sources...',
  },
  involvedName: {
    id: 'involvedName',
    label: 'Resource Name',
    inputMode: 'text',
    placeholder: 'Enter resource name...',
  },
};

/**
 * Helper function to convert ISO string to datetime-local format
 */
const formatDatetimeLocal = (isoString: string): string => {
  const date = new Date(isoString);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  return `${year}-${month}-${day}T${hours}:${minutes}`;
};

/**
 * Check if the current time range matches a preset
 */
const getSelectedPreset = (timeRange: TimeRange): string => {
  const preset = TIME_PRESETS.find((p) => timeRange.start === p.key);
  return preset ? preset.key : 'custom';
};

/**
 * EventsFeedFilters provides filter controls for the events feed
 */
export function EventsFeedFilters({
  client,
  filters,
  timeRange,
  onFiltersChange,
  onTimeRangeChange,
  disabled = false,
  hiddenFilters = [],
  className = '',
  namespace,
}: EventsFeedFiltersProps) {
  const { involvedKinds, reasons, namespaces, sourceComponents, error: facetsError } = useEventFacets(client, timeRange, filters);

  // Log facets error for debugging
  if (facetsError) {
    console.error('Failed to load event facets:', facetsError);
  }

  // Track which filter was just added to auto-open it
  const [pendingFilter, setPendingFilter] = useState<FilterId | null>(null);

  // Custom time range state
  const selectedPreset = getSelectedPreset(timeRange);
  const [customStart, setCustomStart] = useState(() => {
    if (selectedPreset === 'custom') {
      return formatDatetimeLocal(timeRange.start);
    }
    return formatDatetimeLocal(formatISO(subDays(new Date(), 1)));
  });
  const [customEnd, setCustomEnd] = useState(() => {
    if (selectedPreset === 'custom' && timeRange.end) {
      return formatDatetimeLocal(timeRange.end);
    }
    return formatDatetimeLocal(formatISO(new Date()));
  });

  // Handle event type change
  const handleEventTypeChange = useCallback(
    (value: EventTypeOption) => {
      onFiltersChange({
        ...filters,
        eventType: value === 'all' ? undefined : value,
      });
    },
    [filters, onFiltersChange]
  );

  // Handle time range preset selection
  const handleTimePresetSelect = useCallback(
    (presetKey: string) => {
      onTimeRangeChange({
        start: presetKey,
        end: undefined,
      });
    },
    [onTimeRangeChange]
  );

  // Handle custom time range apply
  const handleCustomRangeApply = useCallback(
    (start: string, end: string) => {
      setCustomStart(start);
      setCustomEnd(end);
      onTimeRangeChange({
        start: new Date(start).toISOString(),
        end: new Date(end).toISOString(),
      });
    },
    [onTimeRangeChange]
  );

  // Get display label for time range
  const getTimeRangeLabel = () => {
    const preset = TIME_PRESETS.find((p) => p.key === selectedPreset);
    if (preset) return preset.label;
    if (selectedPreset === 'custom' && timeRange.start && timeRange.end) {
      const start = new Date(timeRange.start);
      const end = new Date(timeRange.end);
      return `${start.toLocaleDateString()} - ${end.toLocaleDateString()}`;
    }
    return 'Select time range';
  };

  // Get current event type value for toggle
  const eventTypeValue: EventTypeOption = filters.eventType || 'all';

  // Determine which filters are currently active (have values) and not hidden
  const filtersWithValues: FilterId[] = [];
  if (filters.involvedKinds && filters.involvedKinds.length > 0 && !hiddenFilters.includes('involvedKinds')) filtersWithValues.push('involvedKinds');
  if (filters.reasons && filters.reasons.length > 0 && !hiddenFilters.includes('reasons')) filtersWithValues.push('reasons');
  if (!namespace && filters.namespaces && filters.namespaces.length > 0 && !hiddenFilters.includes('namespaces')) filtersWithValues.push('namespaces');
  if (filters.sourceComponents && filters.sourceComponents.length > 0 && !hiddenFilters.includes('sourceComponents')) filtersWithValues.push('sourceComponents');
  if (filters.involvedName && !hiddenFilters.includes('involvedName')) filtersWithValues.push('involvedName');

  // Include pendingFilter (newly added filter awaiting value selection) in the displayed filters
  const activeFilterIds: FilterId[] = pendingFilter && !filtersWithValues.includes(pendingFilter)
    ? [...filtersWithValues, pendingFilter]
    : filtersWithValues;

  // Clear pending filter when filter values change (user selected something)
  useEffect(() => {
    if (pendingFilter && filtersWithValues.includes(pendingFilter)) {
      // Filter now has values, clear pending state
      setPendingFilter(null);
    }
  }, [pendingFilter, filtersWithValues]);

  // Build available filters list (exclude namespace if scoped, exclude hidden filters)
  const availableFilters: FilterOption[] = [
    { id: 'involvedKinds', label: 'Kind' },
    { id: 'reasons', label: 'Reason' },
    ...(namespace ? [] : [{ id: 'namespaces' as const, label: 'Namespace' }]),
    { id: 'sourceComponents', label: 'Source' },
    { id: 'involvedName', label: 'Resource Name' },
  ].filter((filter) => !hiddenFilters.includes(filter.id as FilterId));

  // Handle adding a filter
  const handleAddFilter = useCallback((filterId: string) => {
    setPendingFilter(filterId as FilterId);
  }, []);

  // Handle popover close - clear pending filter if no values were selected
  const handlePopoverClose = useCallback(
    (filterId: FilterId) => {
      if (pendingFilter === filterId) {
        const hasValues = (() => {
          const value = filters[filterId];
          if (filterId === 'involvedName') return !!value;
          return Array.isArray(value) && value.length > 0;
        })();
        if (!hasValues) {
          setPendingFilter(null);
        }
      }
    },
    [pendingFilter, filters]
  );

  // Handle filter value changes
  const handleFilterChange = useCallback(
    (filterId: FilterId, values: string[]) => {
      onFiltersChange({
        ...filters,
        [filterId]: values.length > 0 ? values : undefined,
      });
    },
    [filters, onFiltersChange]
  );

  // Handle filter clear
  const handleFilterClear = useCallback(
    (filterId: FilterId) => {
      onFiltersChange({
        ...filters,
        [filterId]: undefined,
      });
    },
    [filters, onFiltersChange]
  );

  // Get options for a specific filter
  const getFilterOptions = (filterId: FilterId) => {
    switch (filterId) {
      case 'involvedKinds':
        return involvedKinds
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case 'reasons':
        return reasons
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case 'namespaces':
        return namespaces
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case 'sourceComponents':
        return sourceComponents
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      default:
        return [];
    }
  };

  // Get values for a specific filter
  const getFilterValues = (filterId: FilterId): string[] => {
    const value = filters[filterId];
    if (filterId === 'involvedName') {
      return value ? [value as string] : [];
    }
    return (value as string[] | undefined) || [];
  };

  // Handle search input change with debouncing
  const handleSearchChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      const value = event.target.value;
      onFiltersChange({
        ...filters,
        search: value || undefined,
      });
    },
    [filters, onFiltersChange]
  );

  return (
    <div className={`border-b border-border py-4 ${className}`}>
      <div className="flex flex-wrap gap-2 items-center">
        {/* Event Type Toggle */}
        {!hiddenFilters.includes('eventType') && (
          <EventTypeToggle
            value={eventTypeValue}
            onChange={handleEventTypeChange}
            disabled={disabled}
          />
        )}

        {/* Search Input — matches ActivityFeed search styling. */}
        <div className="relative min-w-[200px] flex-1 max-w-xs">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search events..."
            value={filters.search || ''}
            onChange={handleSearchChange}
            disabled={disabled}
            className="pl-8 h-7 text-xs pr-6"
          />
          {filters.search ? (
            <button
              type="button"
              onClick={() => onFiltersChange({ ...filters, search: undefined })}
              className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
              aria-label="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          ) : null}
        </div>

        {/* Active Filter Chips */}
        {activeFilterIds.map((filterId) => {
          const config = FILTER_CONFIGS[filterId];
          return (
            <FilterChip
              key={filterId}
              label={config.label}
              values={getFilterValues(filterId)}
              options={config.inputMode === 'typeahead' ? getFilterOptions(filterId) : undefined}
              onValuesChange={(values) => handleFilterChange(filterId, values)}
              onClear={() => handleFilterClear(filterId)}
              onPopoverClose={() => handlePopoverClose(filterId)}
              inputMode={config.inputMode}
              placeholder={config.placeholder}
              searchPlaceholder={config.searchPlaceholder}
              autoOpen={pendingFilter === filterId}
              disabled={disabled}
            />
          );
        })}

        {/* Add Filter Dropdown */}
        <AddFilterDropdown
          availableFilters={availableFilters}
          activeFilterIds={activeFilterIds}
          onAddFilter={handleAddFilter}
          hasActiveFilters={activeFilterIds.length > 0}
          disabled={disabled}
        />

        {/* Spacer */}
        <div className="flex-1 min-w-[20px]" />

        {/* Time Range Dropdown */}
        <TimeRangeDropdown
          presets={TIME_PRESETS}
          selectedPreset={selectedPreset}
          onPresetSelect={handleTimePresetSelect}
          onCustomRangeApply={handleCustomRangeApply}
          customStart={customStart}
          customEnd={customEnd}
          disabled={disabled}
          displayLabel={getTimeRangeLabel()}
        />
      </div>
    </div>
  );
}
