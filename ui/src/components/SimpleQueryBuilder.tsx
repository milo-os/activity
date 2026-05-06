import { useState, useCallback, useEffect, useRef } from 'react';
import type { AuditLogQuerySpec } from '../types';
import type { ActivityApiClient } from '../api/client';
import { useAuditLogFacets } from '../hooks/useAuditLogFacets';
import {
  subMinutes,
  subHours,
  subDays,
  formatISO
} from 'date-fns';
import { Input } from '@datum-cloud/datum-ui/input';
import { Textarea } from '@datum-cloud/datum-ui/textarea';
import { Button } from '@datum-cloud/datum-ui/button';
import { Label } from '@datum-cloud/datum-ui/label';
import { Combobox } from './ui/combobox';
import { Checkbox } from '@datum-cloud/datum-ui/checkbox';
import { Separator } from '@datum-cloud/datum-ui/separator';
import { TimeRangeDropdown } from './ui/time-range-dropdown';

export interface SimpleQueryBuilderProps {
  /** API client instance for fetching facets */
  client: ActivityApiClient;
  onFilterChange: (spec: AuditLogQuerySpec) => void;
  initialLimit?: number;
  className?: string;
  disabled?: boolean;
}

interface SimpleFilters {
  verb: string;
  resourceType: string;
  namespace: string;
  resourceName: string;
  username: string;
}

interface DateTimeRange {
  start: string;
  end: string;
}

type PresetKey =
  | 'last15min'
  | 'last1hour'
  | 'last6hours'
  | 'last24hours'
  | 'last7days'
  | 'last30days';

const TIME_PRESETS: { key: PresetKey; label: string; getValue: () => DateTimeRange }[] = [
  {
    key: 'last15min',
    label: 'Last 15 min',
    getValue: () => ({
      start: formatISO(subMinutes(new Date(), 15)),
      end: formatISO(new Date()),
    }),
  },
  {
    key: 'last1hour',
    label: 'Last hour',
    getValue: () => ({
      start: formatISO(subHours(new Date(), 1)),
      end: formatISO(new Date()),
    }),
  },
  {
    key: 'last6hours',
    label: 'Last 6 hours',
    getValue: () => ({
      start: formatISO(subHours(new Date(), 6)),
      end: formatISO(new Date()),
    }),
  },
  {
    key: 'last24hours',
    label: 'Last 24 hours',
    getValue: () => ({
      start: formatISO(subHours(new Date(), 24)),
      end: formatISO(new Date()),
    }),
  },
  {
    key: 'last7days',
    label: 'Last 7 days',
    getValue: () => ({
      start: formatISO(subDays(new Date(), 7)),
      end: formatISO(new Date()),
    }),
  },
  {
    key: 'last30days',
    label: 'Last 30 days',
    getValue: () => ({
      start: formatISO(subDays(new Date(), 30)),
      end: formatISO(new Date()),
    }),
  },
];

/** Debounce delay for text inputs (ms) */
const DEBOUNCE_DELAY = 300;

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

export function SimpleQueryBuilder({
  client,
  onFilterChange,
  initialLimit = 100,
  className = '',
  disabled = false,
}: SimpleQueryBuilderProps) {
  const [isAdvancedExpanded, setIsAdvancedExpanded] = useState(false);
  const [filters, setFilters] = useState<SimpleFilters>({
    verb: '',
    resourceType: '',
    namespace: '',
    resourceName: '',
    username: '',
  });
  const [limit, setLimit] = useState(initialLimit);
  const [advancedFilter, setAdvancedFilter] = useState('');
  const [useAdvancedFilter, setUseAdvancedFilter] = useState(false);

  // Time range state
  const [selectedPreset, setSelectedPreset] = useState<PresetKey | 'custom'>('last24hours');
  const [timeRange, setTimeRange] = useState<DateTimeRange | null>(null);
  const [customStart, setCustomStart] = useState(() =>
    formatDatetimeLocal(formatISO(subDays(new Date(), 1)))
  );
  const [customEnd, setCustomEnd] = useState(() =>
    formatDatetimeLocal(formatISO(new Date()))
  );

  // Fetch facets for dynamic filter options
  const { verbs, resources, namespaces, usernames, isLoading: facetsLoading, error: facetsError } = useAuditLogFacets(client, timeRange);

  // Log facets error for debugging
  if (facetsError) {
    console.error('Failed to load audit log facets:', facetsError);
  }

  // Refs for debouncing
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isInitialMount = useRef(true);

  // Initialize with default time range and trigger initial query
  useEffect(() => {
    const defaultRange = TIME_PRESETS.find(p => p.key === 'last24hours')?.getValue();
    if (defaultRange) {
      setTimeRange(defaultRange);
    }
  }, []);

  const generateCEL = useCallback((): string => {
    const parts: string[] = [];

    if (filters.verb) {
      parts.push(`verb == "${filters.verb}"`);
    }
    if (filters.resourceType) {
      parts.push(`objectRef.resource == "${filters.resourceType}"`);
    }
    if (filters.namespace) {
      parts.push(`objectRef.namespace == "${filters.namespace}"`);
    }
    if (filters.resourceName) {
      parts.push(`objectRef.name.contains("${filters.resourceName}")`);
    }
    if (filters.username) {
      parts.push(`user.username.contains("${filters.username}")`);
    }

    return parts.join(' && ');
  }, [filters]);

  // Execute query when filters change (with debouncing for text inputs)
  const executeQuery = useCallback(() => {
    if (!timeRange || disabled) return;

    const filter = useAdvancedFilter ? advancedFilter : generateCEL();
    onFilterChange({
      filter,
      limit,
      startTime: timeRange.start,
      endTime: timeRange.end
    });
  }, [useAdvancedFilter, advancedFilter, generateCEL, onFilterChange, limit, timeRange, disabled]);

  // Auto-execute query when dropdown filters or time range change (immediate)
  useEffect(() => {
    // Skip the initial mount - we want to wait for timeRange to be set
    if (isInitialMount.current) {
      isInitialMount.current = false;
      return;
    }

    // Clear any pending debounced query
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }

    executeQuery();
  }, [filters.verb, filters.resourceType, timeRange, limit, useAdvancedFilter]);

  // Auto-execute query when text filters change (debounced)
  useEffect(() => {
    if (isInitialMount.current || !timeRange) return;

    // Clear previous timer
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }

    // Set new debounced timer
    debounceTimerRef.current = setTimeout(() => {
      executeQuery();
    }, DEBOUNCE_DELAY);

    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [filters.namespace, filters.resourceName, filters.username, advancedFilter]);

  const handleFilterChange = useCallback((field: keyof SimpleFilters, value: string) => {
    setFilters(prev => ({ ...prev, [field]: value }));
  }, []);

  const handleTimePresetSelect = useCallback((presetKey: string) => {
    const preset = TIME_PRESETS.find(p => p.key === presetKey);
    if (preset) {
      setSelectedPreset(presetKey as PresetKey);
      setTimeRange(preset.getValue());
    }
  }, []);

  const handleCustomRangeApply = useCallback((start: string, end: string) => {
    setSelectedPreset('custom');
    setCustomStart(start);
    setCustomEnd(end);
    setTimeRange({
      start: new Date(start).toISOString(),
      end: new Date(end).toISOString(),
    });
  }, []);

  const handleLimitChange = useCallback((newLimit: number) => {
    const validLimit = Math.min(Math.max(1, newLimit), 1000);
    setLimit(validLimit);
  }, []);

  const handleAdvancedToggle = useCallback(() => {
    if (!isAdvancedExpanded) {
      // When expanding, populate advanced filter with generated CEL
      setAdvancedFilter(generateCEL());
    }
    setIsAdvancedExpanded(!isAdvancedExpanded);
  }, [isAdvancedExpanded, generateCEL]);

  const handleUseAdvancedChange = useCallback((checked: boolean | 'indeterminate') => {
    setUseAdvancedFilter(checked === true);
  }, []);

  // Get display label for time range
  const getTimeRangeLabel = () => {
    if (selectedPreset === 'custom') {
      if (timeRange?.start && timeRange?.end) {
        const start = new Date(timeRange.start);
        const end = new Date(timeRange.end);
        return `${start.toLocaleDateString()} - ${end.toLocaleDateString()}`;
      }
      return 'Custom';
    }
    const preset = TIME_PRESETS.find(p => p.key === selectedPreset);
    return preset?.label || 'Select time range';
  };

  return (
    <div className={`mb-6 pb-6 border-b border-border ${className}`}>
      {/* Filters Row */}
      <div className="flex flex-wrap gap-4 items-end">
        {/* Action Filter */}
        <div className="flex flex-col gap-2">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Action</Label>
          <Combobox
            options={verbs
              .filter(facet => facet.value)
              .map(facet => ({
                value: facet.value,
                label: facet.value,
                count: facet.count,
              }))}
            value={filters.verb}
            onValueChange={(value) => handleFilterChange('verb', value)}
            placeholder="All"
            searchPlaceholder="Search actions..."
            allOptionLabel="All"
            disabled={disabled || useAdvancedFilter}
            loading={facetsLoading}
            className="min-w-[130px]"
          />
        </div>

        {/* Resource Type Filter */}
        <div className="flex flex-col gap-2">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Resource</Label>
          <Combobox
            options={resources
              .filter(facet => facet.value)
              .map(facet => ({
                value: facet.value,
                label: facet.value,
                count: facet.count,
              }))}
            value={filters.resourceType}
            onValueChange={(value) => handleFilterChange('resourceType', value)}
            placeholder="All"
            searchPlaceholder="Search resources..."
            allOptionLabel="All"
            disabled={disabled || useAdvancedFilter}
            loading={facetsLoading}
            className="min-w-[130px]"
          />
        </div>

        {/* Namespace Filter */}
        <div className="flex flex-col gap-2">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Namespace</Label>
          <Combobox
            options={namespaces
              .filter(facet => facet.value)
              .map(facet => ({
                value: facet.value,
                label: facet.value,
                count: facet.count,
              }))}
            value={filters.namespace}
            onValueChange={(value) => handleFilterChange('namespace', value)}
            placeholder="All"
            searchPlaceholder="Search namespaces..."
            allOptionLabel="All"
            disabled={disabled || useAdvancedFilter}
            loading={facetsLoading}
            className="min-w-[130px]"
          />
        </div>

        {/* Username Filter */}
        <div className="flex flex-col gap-2">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">User</Label>
          <Combobox
            options={usernames
              .filter(facet => facet.value)
              .map(facet => ({
                value: facet.value,
                label: facet.value,
                count: facet.count,
              }))}
            value={filters.username}
            onValueChange={(value) => handleFilterChange('username', value)}
            placeholder="All"
            searchPlaceholder="Search users..."
            allOptionLabel="All"
            disabled={disabled || useAdvancedFilter}
            loading={facetsLoading}
            className="min-w-[130px]"
          />
        </div>

        {/* Resource Name */}
        <div className="flex flex-col gap-2">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Name</Label>
          <Input
            type="text"
            value={filters.resourceName}
            onChange={(e) => handleFilterChange('resourceName', e.target.value)}
            placeholder="Filter by name..."
            className="min-w-[140px]"
            disabled={disabled || useAdvancedFilter}
          />
        </div>

        {/* Results Limit */}
        <div className="flex flex-col gap-2">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Limit</Label>
          <Input
            type="number"
            value={limit}
            onChange={(e) => handleLimitChange(parseInt(e.target.value) || 100)}
            min={1}
            max={1000}
            className="w-[80px]"
            disabled={disabled}
          />
        </div>

        {/* Toggle Advanced Filters */}
        <Button
          htmlType="button"
          type="quaternary" theme="borderless"
          size="small"
          className="whitespace-nowrap text-muted-foreground hover:text-foreground"
          onClick={handleAdvancedToggle}
          aria-expanded={isAdvancedExpanded}
        >
          {isAdvancedExpanded ? '- CEL' : '+ CEL'}
        </Button>

        {/* Time Range Dropdown - Right aligned */}
        <div className="flex flex-col gap-2 ml-auto">
          <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Time Range</Label>
          <TimeRangeDropdown
            presets={TIME_PRESETS.map(p => ({ key: p.key, label: p.label }))}
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

      {/* Advanced Filters (Expandable) */}
      {isAdvancedExpanded && (
        <div className="mt-4 pt-4">
          <Separator className="mb-4" />
          <div className="mb-3">
            <div className="flex items-center gap-2">
              <Checkbox
                id="use-cel-expression"
                checked={useAdvancedFilter}
                onCheckedChange={handleUseAdvancedChange}
                disabled={disabled}
              />
              <Label
                htmlFor="use-cel-expression"
                className="text-sm font-medium text-foreground cursor-pointer"
              >
                Use CEL Expression
              </Label>
            </div>
          </div>
          {useAdvancedFilter && (
            <div className="mt-3">
              <p className="m-0 mb-2 text-sm text-muted-foreground">
                Write your query using CEL syntax for complex filters
              </p>
              <Textarea
                value={advancedFilter}
                onChange={(e) => setAdvancedFilter(e.target.value)}
                placeholder='Example: verb == "delete" && objectRef.namespace == "production"'
                rows={3}
                className="w-full font-mono resize-y"
                disabled={disabled}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
