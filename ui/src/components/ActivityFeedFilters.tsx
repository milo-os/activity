import { useState, useCallback, useEffect, useRef, useMemo } from "react";
import { formatISO, subDays } from "date-fns";
import { Search, X } from "lucide-react";

import type { ActivityFeedFilters as FilterState } from "../hooks/useActivityFeed";
import type { TimeRange } from "../hooks/useActivityFeed";
import type { ActivityApiClient } from "../api/client";
import { useFacets } from "../hooks/useFacets";
import { ChangeSourceToggle, ChangeSourceOption } from "./ChangeSourceToggle";
import { TimeRangeDropdown } from "./ui/time-range-dropdown";
import { FilterChip } from "./ui/filter-chip";
import { AddFilterDropdown, type FilterOption } from "./ui/add-filter-dropdown";
import { Input } from "@datum-cloud/datum-ui/input";

export interface ActivityFeedFiltersProps {
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
  hiddenFilters?: Array<
    | "resourceKinds"
    | "actorNames"
    | "apiGroups"
    | "resourceNamespaces"
    | "resourceName"
    | "actions"
    | "changeSource"
  >;
  /** Additional CSS class */
  className?: string;
}

/**
 * Preset time ranges
 */
const TIME_PRESETS = [
  { key: "now-1h", label: "Last hour" },
  { key: "now-24h", label: "Last 24 hours" },
  { key: "now-7d", label: "Last 7 days" },
  { key: "now-30d", label: "Last 30 days" },
];

/**
 * Filter configuration registry
 */
type FilterId =
  | "resourceKinds"
  | "actorNames"
  | "apiGroups"
  | "resourceNamespaces"
  | "resourceName"
  | "actions";

interface FilterConfig {
  id: FilterId;
  label: string;
  inputMode: "typeahead" | "text";
  placeholder?: string;
  searchPlaceholder?: string;
}

const FILTER_CONFIGS: Record<FilterId, FilterConfig> = {
  resourceKinds: {
    id: "resourceKinds",
    label: "Kind",
    inputMode: "typeahead",
    searchPlaceholder: "Search kinds...",
  },
  actorNames: {
    id: "actorNames",
    label: "Actor",
    inputMode: "typeahead",
    searchPlaceholder: "Search actors...",
  },
  apiGroups: {
    id: "apiGroups",
    label: "API Group",
    inputMode: "typeahead",
    searchPlaceholder: "Search API groups...",
  },
  resourceNamespaces: {
    id: "resourceNamespaces",
    label: "Namespace",
    inputMode: "typeahead",
    searchPlaceholder: "Search namespaces...",
  },
  resourceName: {
    id: "resourceName",
    label: "Resource Name",
    inputMode: "text",
    placeholder: "Enter resource name...",
  },
  actions: {
    id: "actions",
    label: "Action",
    inputMode: "typeahead",
    searchPlaceholder: "Search actions...",
  },
};

/**
 * Helper function to convert ISO string to datetime-local format
 */
const formatDatetimeLocal = (isoString: string): string => {
  const date = new Date(isoString);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  return `${year}-${month}-${day}T${hours}:${minutes}`;
};

/**
 * Check if the current time range matches a preset
 */
const getSelectedPreset = (timeRange: TimeRange): string => {
  const preset = TIME_PRESETS.find((p) => timeRange.start === p.key);
  return preset ? preset.key : "custom";
};

/**
 * ActivityFeedFilters provides filter controls for the activity feed
 */
export function ActivityFeedFilters({
  client,
  filters,
  timeRange,
  onFiltersChange,
  onTimeRangeChange,
  disabled = false,
  hiddenFilters = [],
  className = "",
}: ActivityFeedFiltersProps) {
  const {
    resourceKinds,
    actorNames,
    apiGroups,
    resourceNamespaces,
    error: facetsError,
  } = useFacets(client, timeRange, filters);

  // Log facets error for debugging
  if (facetsError) {
    console.error("Failed to load facets:", facetsError);
  }

  // Track which filter was just added to auto-open it
  const [pendingFilter, setPendingFilter] = useState<FilterId | null>(null);

  // Custom time range state
  const selectedPreset = getSelectedPreset(timeRange);
  const [customStart, setCustomStart] = useState(() => {
    if (selectedPreset === "custom") {
      return formatDatetimeLocal(timeRange.start);
    }
    return formatDatetimeLocal(formatISO(subDays(new Date(), 1)));
  });
  const [customEnd, setCustomEnd] = useState(() => {
    if (selectedPreset === "custom" && timeRange.end) {
      return formatDatetimeLocal(timeRange.end);
    }
    return formatDatetimeLocal(formatISO(new Date()));
  });

  // Handle change source change
  const handleChangeSourceChange = useCallback(
    (value: ChangeSourceOption) => {
      onFiltersChange({
        ...filters,
        changeSource: value,
      });
    },
    [filters, onFiltersChange],
  );

  // Handle time range preset selection
  const handleTimePresetSelect = useCallback(
    (presetKey: string) => {
      onTimeRangeChange({
        start: presetKey,
        end: undefined,
      });
    },
    [onTimeRangeChange],
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
    [onTimeRangeChange],
  );

  // Get display label for time range
  const getTimeRangeLabel = () => {
    const preset = TIME_PRESETS.find((p) => p.key === selectedPreset);
    if (preset) return preset.label;
    if (selectedPreset === "custom" && timeRange.start && timeRange.end) {
      const start = new Date(timeRange.start);
      const end = new Date(timeRange.end);
      return `${start.toLocaleDateString()} - ${end.toLocaleDateString()}`;
    }
    return "Select time range";
  };

  // Determine which filters are currently active (have values) and not hidden
  const filtersWithValues = useMemo<FilterId[]>(() => {
    const result: FilterId[] = [];
    if (
      filters.resourceKinds &&
      filters.resourceKinds.length > 0 &&
      !hiddenFilters.includes("resourceKinds")
    )
      result.push("resourceKinds");
    if (
      filters.actorNames &&
      filters.actorNames.length > 0 &&
      !hiddenFilters.includes("actorNames")
    )
      result.push("actorNames");
    if (
      filters.apiGroups &&
      filters.apiGroups.length > 0 &&
      !hiddenFilters.includes("apiGroups")
    )
      result.push("apiGroups");
    if (
      filters.resourceNamespaces &&
      filters.resourceNamespaces.length > 0 &&
      !hiddenFilters.includes("resourceNamespaces")
    )
      result.push("resourceNamespaces");
    if (filters.resourceName && !hiddenFilters.includes("resourceName"))
      result.push("resourceName");
    if (filters.actions && filters.actions.length > 0) result.push("actions");
    return result;
  }, [filters, hiddenFilters]);

  // Include pendingFilter (newly added filter awaiting value selection) in the displayed filters
  const activeFilterIds: FilterId[] =
    pendingFilter && !filtersWithValues.includes(pendingFilter)
      ? [...filtersWithValues, pendingFilter]
      : filtersWithValues;

  // Clear pending filter when filter values change (user selected something)
  useEffect(() => {
    if (pendingFilter && filtersWithValues.includes(pendingFilter)) {
      // Filter now has values, clear pending state
      setPendingFilter(null);
    }
  }, [pendingFilter, filtersWithValues]);

  // Build available filters list (exclude hidden filters)
  const availableFilters: FilterOption[] = [
    { id: "resourceKinds", label: "Kind" },
    { id: "actorNames", label: "Actor" },
    { id: "apiGroups", label: "API Group" },
    { id: "resourceNamespaces", label: "Namespace" },
    { id: "resourceName", label: "Resource Name" },
    // 'actions' hidden until backend facet support is available
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
          if (filterId === "resourceName") return !!value;
          return Array.isArray(value) && value.length > 0;
        })();
        if (!hasValues) {
          setPendingFilter(null);
        }
      }
    },
    [pendingFilter, filters],
  );

  // Handle filter value changes
  const handleFilterChange = useCallback(
    (filterId: FilterId, values: string[]) => {
      onFiltersChange({
        ...filters,
        [filterId]: values.length > 0 ? values : undefined,
      });
    },
    [filters, onFiltersChange],
  );

  // Handle filter clear
  const handleFilterClear = useCallback(
    (filterId: FilterId) => {
      onFiltersChange({
        ...filters,
        [filterId]: undefined,
      });
    },
    [filters, onFiltersChange],
  );

  // Get options for a specific filter
  const getFilterOptions = (filterId: FilterId) => {
    switch (filterId) {
      case "resourceKinds":
        return resourceKinds
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "actorNames":
        return actorNames
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "apiGroups":
        return apiGroups
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "resourceNamespaces":
        return resourceNamespaces
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "actions":
        // TODO: Return action facets when backend supports it
        return [];
      default:
        return [];
    }
  };

  // Get values for a specific filter
  const getFilterValues = (filterId: FilterId): string[] => {
    const value = filters[filterId];
    if (filterId === "resourceName") {
      return value ? [value as string] : [];
    }
    if (filterId === "actions") {
      return (value as string[] | undefined) || [];
    }
    return (value as string[] | undefined) || [];
  };

  // Local search value for debouncing — keeps input responsive while query runs
  const [searchInputValue, setSearchInputValue] = useState(
    filters.search || "",
  );
  const searchDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Use refs so the debounced callback never closes over stale values
  const filtersRef = useRef(filters);
  filtersRef.current = filters;
  const onFiltersChangeRef = useRef(onFiltersChange);
  onFiltersChangeRef.current = onFiltersChange;

  // Cancel any pending debounce on unmount
  useEffect(() => {
    return () => {
      if (searchDebounceRef.current) clearTimeout(searchDebounceRef.current);
    };
  }, []);

  const handleSearchChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      const value = event.target.value;
      setSearchInputValue(value);
      if (searchDebounceRef.current) clearTimeout(searchDebounceRef.current);
      searchDebounceRef.current = setTimeout(() => {
        onFiltersChangeRef.current({
          ...filtersRef.current,
          search: value || undefined,
        });
      }, 400);
    },
    [],
  );

  const handleSearchClear = useCallback(() => {
    setSearchInputValue("");
    if (searchDebounceRef.current) clearTimeout(searchDebounceRef.current);
    onFiltersChangeRef.current({ ...filtersRef.current, search: undefined });
  }, []);

  return (
    <div className={`border-b border-border py-4 ${className}`}>
      <div className="flex flex-wrap gap-2 items-center">
        {/* Change Source Toggle */}
        {!hiddenFilters.includes("changeSource") && (
          <ChangeSourceToggle
            value={filters.changeSource || "all"}
            onChange={handleChangeSourceChange}
            disabled={disabled}
          />
        )}

        {/* Search Input */}
        <div className="relative min-w-[200px] flex-1 max-w-xs">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search activities..."
            value={searchInputValue}
            onChange={handleSearchChange}
            className="pl-8 h-7 text-xs pr-6"
          />
          {searchInputValue && (
            <button
              onClick={handleSearchClear}
              className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
              aria-label="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        {/* Active Filter Chips */}
        {activeFilterIds.map((filterId) => {
          const config = FILTER_CONFIGS[filterId];
          return (
            <FilterChip
              key={filterId}
              label={config.label}
              values={getFilterValues(filterId)}
              options={
                config.inputMode === "typeahead"
                  ? getFilterOptions(filterId)
                  : undefined
              }
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
