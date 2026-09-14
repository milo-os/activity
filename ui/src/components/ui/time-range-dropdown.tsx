import * as React from 'react';
import { DateTimeRangePicker, type PickerPreset } from '@datum-cloud/datum-ui/picker';

export interface TimeRangePreset {
  key: string;
  label: string;
}

export interface TimeRangeDropdownProps {
  /** Available time range presets, keyed by relative strings such as `now-24h`. */
  presets: TimeRangePreset[];
  /** Currently selected preset key, or 'custom' for custom range */
  selectedPreset: string;
  /** Handler when a preset is selected */
  onPresetSelect: (presetKey: string) => void;
  /** Handler when custom range is applied; values are `datetime-local` strings */
  onCustomRangeApply: (start: string, end: string) => void;
  /** Initial custom start value (datetime-local format) */
  customStart?: string;
  /** Initial custom end value (datetime-local format) */
  customEnd?: string;
  /** Whether the dropdown is disabled */
  disabled?: boolean;
  /** Additional class name */
  className?: string;
  /** Display label for the selected value */
  displayLabel?: string;
}

const RELATIVE_KEY = /^now-(\d+)([mhd])$/;

/** Resolve a relative key such as `now-7d` to a `{ from, to }` window ending now. */
export function relativeKeyToRange(key: string, now: Date = new Date()): { from: Date; to: Date } | null {
  const match = RELATIVE_KEY.exec(key);
  if (!match) return null;
  const amount = Number(match[1]);
  const unitMs = { m: 60_000, h: 3_600_000, d: 86_400_000 }[match[2] as 'm' | 'h' | 'd'];
  return { from: new Date(now.getTime() - amount * unitMs), to: now };
}

/** `YYYY-MM-DDTHH:mm` in local time, the format the filter components store. */
export function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function toPickerPresets(presets: TimeRangePreset[]): PickerPreset[] {
  return presets
    .filter((p) => RELATIVE_KEY.test(p.key))
    .map((p) => ({
      key: p.key,
      label: p.label,
      getRange: () => relativeKeyToRange(p.key)!,
    }));
}

/**
 * TimeRangeDropdown - relative presets plus an absolute range, on datum-ui's
 * DateTimeRangePicker. The emitted value carries `preset` when a preset was
 * clicked, which keeps `start=now-7d` style URL state intact.
 */
export function TimeRangeDropdown({
  presets,
  selectedPreset,
  onPresetSelect,
  onCustomRangeApply,
  customStart,
  customEnd,
  disabled = false,
  className,
  displayLabel,
}: TimeRangeDropdownProps) {
  const pickerPresets = React.useMemo(() => toPickerPresets(presets), [presets]);

  const value = React.useMemo(() => {
    if (selectedPreset !== 'custom') {
      const range = relativeKeyToRange(selectedPreset);
      return range
        ? { from: range.from.toISOString(), to: range.to.toISOString(), preset: selectedPreset }
        : null;
    }
    if (customStart && customEnd) {
      return { from: new Date(customStart).toISOString(), to: new Date(customEnd).toISOString() };
    }
    return null;
  }, [selectedPreset, customStart, customEnd]);

  const handleChange = (next: { from: string; to: string } | null) => {
    if (!next) return;
    const preset = (next as { preset?: string }).preset;
    if (preset) {
      onPresetSelect(preset);
      return;
    }
    onCustomRangeApply(toDatetimeLocal(next.from), toDatetimeLocal(next.to));
  };

  return (
    <DateTimeRangePicker
      className={className}
      value={value}
      onChange={handleChange}
      presets={pickerPresets}
      disableFuture
      clearable={false}
      disabled={disabled}
      placeholder={displayLabel ?? 'Select time range'}
      triggerLabel={displayLabel ? () => displayLabel : undefined}
    />
  );
}
