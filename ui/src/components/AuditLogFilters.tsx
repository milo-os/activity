import { useState, useCallback, useEffect } from "react";
import { formatISO, subDays } from "date-fns";

import type { ActivityApiClient } from "../api/client";
import {
  useAuditLogFacets,
  type AuditLogTimeRange,
} from "../hooks/useAuditLogFacets";
import { TimeRangeDropdown } from "./ui/time-range-dropdown";
import { FilterChip } from "./ui/filter-chip";
import { AddFilterDropdown, type FilterOption } from "./ui/add-filter-dropdown";
import { ActionMultiSelect } from "./ActionMultiSelect";
import { UserSelect } from "./UserSelect";

/**
 * Filter state for audit logs
 */
export interface AuditLogFilterState {
  /** Filter by verb/action (multi-select) */
  verbs?: string[];
  /** Filter by resource type (multi-select) */
  resourceTypes?: string[];
  /** Filter by namespace (multi-select) */
  namespaces?: string[];
  /** Filter by username (multi-select) */
  usernames?: string[];
  /** Filter by resource name (partial match) */
  resourceName?: string;
  /** Custom CEL filter */
  customFilter?: string;
}

/**
 * Time range for audit log queries
 */
export interface TimeRange {
  start: string;
  end?: string;
}

export interface AuditLogFiltersProps {
  /** API client instance for fetching facets */
  client: ActivityApiClient;
  /** Current filter state */
  filters: AuditLogFilterState;
  /** Current time range */
  timeRange: TimeRange;
  /** Handler called when filters change */
  onFiltersChange: (filters: AuditLogFilterState) => void;
  /** Handler called when time range changes */
  onTimeRangeChange: (timeRange: TimeRange) => void;
  /** Whether the filters are disabled (e.g., during loading) */
  disabled?: boolean;
  /** Additional CSS class */
  className?: string;
}

/**
 * Preset time ranges
 */
const TIME_PRESETS = [
  { key: "last15min", label: "Last 15 min" },
  { key: "last1hour", label: "Last hour" },
  { key: "last6hours", label: "Last 6 hours" },
  { key: "last24hours", label: "Last 24 hours" },
  { key: "last7days", label: "Last 7 days" },
  { key: "last30days", label: "Last 30 days" },
];

/**
 * Convert preset key to ISO time range
 */
function presetToTimeRange(presetKey: string): AuditLogTimeRange {
  const now = new Date();
  let start: Date;

  switch (presetKey) {
    case "last15min":
      start = new Date(now.getTime() - 15 * 60 * 1000);
      break;
    case "last1hour":
      start = new Date(now.getTime() - 60 * 60 * 1000);
      break;
    case "last6hours":
      start = new Date(now.getTime() - 6 * 60 * 60 * 1000);
      break;
    case "last24hours":
      start = new Date(now.getTime() - 24 * 60 * 60 * 1000);
      break;
    case "last7days":
      start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
      break;
    case "last30days":
      start = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
      break;
    default:
      start = new Date(now.getTime() - 24 * 60 * 60 * 1000);
  }

  return {
    start: formatISO(start),
    end: formatISO(now),
  };
}

/**
 * Filter configuration registry
 */
type FilterId =
  | "verbs"
  | "resourceTypes"
  | "namespaces"
  | "usernames"
  | "resourceName";

interface FilterConfig {
  id: FilterId;
  label: string;
  inputMode: "typeahead" | "text";
  placeholder?: string;
  searchPlaceholder?: string;
}

const FILTER_CONFIGS: Record<FilterId, FilterConfig> = {
  verbs: {
    id: "verbs",
    label: "Action",
    inputMode: "typeahead",
    searchPlaceholder: "Search actions...",
  },
  resourceTypes: {
    id: "resourceTypes",
    label: "Resource",
    inputMode: "typeahead",
    searchPlaceholder: "Search resources...",
  },
  namespaces: {
    id: "namespaces",
    label: "Namespace",
    inputMode: "typeahead",
    searchPlaceholder: "Search namespaces...",
  },
  usernames: {
    id: "usernames",
    label: "User",
    inputMode: "typeahead",
    searchPlaceholder: "Search users...",
  },
  resourceName: {
    id: "resourceName",
    label: "Name",
    inputMode: "text",
    placeholder: "Enter resource name...",
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
 * Build CEL filter expression from filter state
 */
export function buildAuditLogCEL(filters: AuditLogFilterState): string {
  const conditions: string[] = [];

  // Verbs filter (multi-select)
  if (filters.verbs && filters.verbs.length > 0) {
    if (filters.verbs.length === 1) {
      conditions.push(`verb == "${filters.verbs[0]}"`);
    } else {
      const verbConditions = filters.verbs.map((v) => `verb == "${v}"`);
      conditions.push(`(${verbConditions.join(" || ")})`);
    }
  }

  // Resource types filter (multi-select)
  if (filters.resourceTypes && filters.resourceTypes.length > 0) {
    if (filters.resourceTypes.length === 1) {
      conditions.push(`objectRef.resource == "${filters.resourceTypes[0]}"`);
    } else {
      const resConditions = filters.resourceTypes.map(
        (r) => `objectRef.resource == "${r}"`,
      );
      conditions.push(`(${resConditions.join(" || ")})`);
    }
  }

  // Namespaces filter (multi-select)
  if (filters.namespaces && filters.namespaces.length > 0) {
    if (filters.namespaces.length === 1) {
      conditions.push(`objectRef.namespace == "${filters.namespaces[0]}"`);
    } else {
      const nsConditions = filters.namespaces.map(
        (ns) => `objectRef.namespace == "${ns}"`,
      );
      conditions.push(`(${nsConditions.join(" || ")})`);
    }
  }

  // Usernames filter (multi-select)
  if (filters.usernames && filters.usernames.length > 0) {
    if (filters.usernames.length === 1) {
      conditions.push(`user.username == "${filters.usernames[0]}"`);
    } else {
      const userConditions = filters.usernames.map(
        (u) => `user.username == "${u}"`,
      );
      conditions.push(`(${userConditions.join(" || ")})`);
    }
  }

  // Resource name filter (partial match)
  if (filters.resourceName) {
    conditions.push(`objectRef.name.contains("${filters.resourceName}")`);
  }

  // Custom filter
  if (filters.customFilter) {
    conditions.push(filters.customFilter);
  }

  return conditions.join(" && ");
}

/**
 * AuditLogFilters provides compact filter controls for audit log queries
 */
export function AuditLogFilters({
  client,
  filters,
  timeRange,
  onFiltersChange,
  onTimeRangeChange,
  disabled = false,
  className = "",
}: AuditLogFiltersProps) {
  // Convert timeRange to format expected by useAuditLogFacets
  const [facetTimeRange, setFacetTimeRange] =
    useState<AuditLogTimeRange | null>(() => presetToTimeRange("last24hours"));

  const {
    verbs,
    resources,
    namespaces,
    usernames,
    error: facetsError,
  } = useAuditLogFacets(client, facetTimeRange);

  // Log facets error for debugging
  if (facetsError) {
    console.error("Failed to load audit log facets:", facetsError);
  }

  // Track which filter was just added to auto-open it
  const [pendingFilter, setPendingFilter] = useState<FilterId | null>(null);

  // Track selected preset
  const [selectedPreset, setSelectedPreset] = useState<string>("last24hours");

  // Custom time range state
  const [customStart, setCustomStart] = useState(() =>
    formatDatetimeLocal(formatISO(subDays(new Date(), 1))),
  );
  const [customEnd, setCustomEnd] = useState(() =>
    formatDatetimeLocal(formatISO(new Date())),
  );

  // Handle time range preset selection
  const handleTimePresetSelect = useCallback(
    (presetKey: string) => {
      setSelectedPreset(presetKey);
      const range = presetToTimeRange(presetKey);
      setFacetTimeRange(range);
      onTimeRangeChange({
        start: range.start,
        end: range.end,
      });
    },
    [onTimeRangeChange],
  );

  // Handle custom time range apply
  const handleCustomRangeApply = useCallback(
    (start: string, end: string) => {
      setSelectedPreset("custom");
      setCustomStart(start);
      setCustomEnd(end);
      const startIso = new Date(start).toISOString();
      const endIso = new Date(end).toISOString();
      setFacetTimeRange({ start: startIso, end: endIso });
      onTimeRangeChange({
        start: startIso,
        end: endIso,
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

  // Determine which filters are currently active (have values)
  // Note: We exclude verbs and usernames from filter chips since they're handled by quick filters
  const filtersWithValues: FilterId[] = [];
  // if (filters.verbs && filters.verbs.length > 0) filtersWithValues.push('verbs'); // Handled by ActionToggle
  if (filters.resourceTypes && filters.resourceTypes.length > 0)
    filtersWithValues.push("resourceTypes");
  if (filters.namespaces && filters.namespaces.length > 0)
    filtersWithValues.push("namespaces");
  // if (filters.usernames && filters.usernames.length > 0) filtersWithValues.push('usernames'); // Handled by UserSelect
  if (filters.resourceName) filtersWithValues.push("resourceName");

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

  // Build available filters list
  // Note: Action and User are now quick filters, so they're excluded from the dropdown
  const availableFilters: FilterOption[] = [
    { id: "resourceTypes", label: "Resource" },
    { id: "namespaces", label: "Namespace" },
    { id: "resourceName", label: "Name" },
  ];

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
      case "verbs":
        return verbs
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "resourceTypes":
        return resources
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "namespaces":
        return namespaces
          .filter((facet) => facet.value)
          .map((facet) => ({
            value: facet.value,
            label: facet.value,
            count: facet.count,
          }));
      case "usernames":
        return usernames
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
    if (filterId === "resourceName") {
      return value ? [value as string] : [];
    }
    return (value as string[] | undefined) || [];
  };

  // Handle action multi-select change
  const handleActionChange = useCallback(
    (selectedVerbs: string[]) => {
      onFiltersChange({
        ...filters,
        verbs: selectedVerbs.length > 0 ? selectedVerbs : undefined,
      });
    },
    [filters, onFiltersChange],
  );

  // Get current action values for multi-select
  const getActionValues = (): string[] => {
    return filters.verbs || [];
  };

  // Prepare action options from facets
  const actionOptions = verbs
    .filter((facet) => facet.value)
    .map((facet) => ({
      value: facet.value,
      label: facet.value.charAt(0).toUpperCase() + facet.value.slice(1), // Capitalize first letter
      count: facet.count,
    }));

  // Handle user select change
  const handleUserChange = useCallback(
    (username?: string) => {
      onFiltersChange({
        ...filters,
        usernames: username ? [username] : undefined,
      });
    },
    [filters, onFiltersChange],
  );

  // Get current user value for select (single selection for quick filter)
  const getCurrentUser = (): string | undefined => {
    return filters.usernames && filters.usernames.length === 1
      ? filters.usernames[0]
      : undefined;
  };

  // Prepare user options for select
  const userOptions = usernames
    .filter((facet) => facet.value)
    .map((facet) => ({
      value: facet.value,
      label: facet.value,
      count: facet.count,
    }));

  return (
    <div className={`border-b border-border py-4 pt-0 ${className}`}>
      <div className="flex flex-wrap gap-2 items-center">
        {/* Action Multi-Select */}
        <ActionMultiSelect
          value={getActionValues()}
          onChange={handleActionChange}
          options={actionOptions}
          disabled={disabled}
          isLoading={!verbs.length && !facetsError}
          className="h-7 text-xs min-w-[180px]"
        />

        {/* User Select */}
        <UserSelect
          value={getCurrentUser()}
          options={userOptions}
          onChange={handleUserChange}
          disabled={disabled}
          isLoading={!usernames.length && !facetsError}
        />

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
