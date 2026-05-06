import * as React from 'react';
import { Calendar, ChevronDown } from 'lucide-react';
import * as Popover from '@radix-ui/react-popover';
import { cn } from '../../lib/utils';
import { Button } from '@datum-cloud/datum-ui/button';
import { Input } from '@datum-cloud/datum-ui/input';
import { Label } from '@datum-cloud/datum-ui/label';

export interface TimeRangePreset {
  key: string;
  label: string;
}

export interface TimeRangeDropdownProps {
  /** Available time range presets */
  presets: TimeRangePreset[];
  /** Currently selected preset key, or 'custom' for custom range */
  selectedPreset: string;
  /** Handler when a preset is selected */
  onPresetSelect: (presetKey: string) => void;
  /** Handler when custom range is applied */
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

/**
 * TimeRangeDropdown - A compact dropdown for selecting time ranges
 */
export function TimeRangeDropdown({
  presets,
  selectedPreset,
  onPresetSelect,
  onCustomRangeApply,
  customStart: initialCustomStart,
  customEnd: initialCustomEnd,
  disabled = false,
  className,
  displayLabel,
}: TimeRangeDropdownProps) {
  const [open, setOpen] = React.useState(false);
  const [showCustomInputs, setShowCustomInputs] = React.useState(false);
  const [customStart, setCustomStart] = React.useState(initialCustomStart || '');
  const [customEnd, setCustomEnd] = React.useState(initialCustomEnd || '');

  // Update custom values when props change
  React.useEffect(() => {
    if (initialCustomStart) setCustomStart(initialCustomStart);
    if (initialCustomEnd) setCustomEnd(initialCustomEnd);
  }, [initialCustomStart, initialCustomEnd]);

  const selectedPresetObj = presets.find((p) => p.key === selectedPreset);
  const label = displayLabel || selectedPresetObj?.label || 'Select time range';

  const handlePresetClick = (presetKey: string) => {
    onPresetSelect(presetKey);
    setShowCustomInputs(false);
    setOpen(false);
  };

  const handleCustomClick = () => {
    setShowCustomInputs(true);
  };

  const handleCustomApply = () => {
    if (customStart && customEnd) {
      onCustomRangeApply(customStart, customEnd);
      setShowCustomInputs(false);
      setOpen(false);
    }
  };

  const handleCustomCancel = () => {
    setShowCustomInputs(false);
  };

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          disabled={disabled}
          className={cn(
            'flex h-7 items-center gap-2 rounded-md border border-input bg-background px-2 text-xs ring-offset-background',
            'hover:bg-accent hover:text-accent-foreground',
            'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
            'disabled:cursor-not-allowed disabled:opacity-50',
            className
          )}
        >
          <Calendar className="h-4 w-4 text-muted-foreground" />
          <span className="whitespace-nowrap">{label}</span>
          <ChevronDown className="h-4 w-4 text-muted-foreground" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className={cn(
            'z-50 min-w-[200px] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
            'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
            'data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2'
          )}
          sideOffset={4}
          align="end"
        >
          {!showCustomInputs ? (
            <div className="p-1">
              {presets.map((preset) => (
                <button
                  key={preset.key}
                  type="button"
                  onClick={() => handlePresetClick(preset.key)}
                  className={cn(
                    'relative flex w-full cursor-pointer select-none items-center rounded-sm px-3 py-2 text-sm outline-none',
                    'hover:bg-accent hover:text-accent-foreground',
                    selectedPreset === preset.key && 'bg-accent text-accent-foreground font-medium'
                  )}
                >
                  {preset.label}
                </button>
              ))}
              <div className="my-1 h-px bg-border" />
              <button
                type="button"
                onClick={handleCustomClick}
                className={cn(
                  'relative flex w-full cursor-pointer select-none items-center rounded-sm px-3 py-2 text-sm outline-none',
                  'hover:bg-accent hover:text-accent-foreground',
                  selectedPreset === 'custom' && 'bg-accent text-accent-foreground font-medium'
                )}
              >
                Custom range...
              </button>
            </div>
          ) : (
            <div className="p-4 min-w-[280px]">
              <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                  <Label
                    htmlFor="time-range-start"
                    className="text-xs text-muted-foreground font-semibold uppercase tracking-tight"
                  >
                    Start
                  </Label>
                  <Input
                    id="time-range-start"
                    type="datetime-local"
                    value={customStart}
                    onChange={(e) => setCustomStart(e.target.value)}
                    className="w-full"
                    disabled={disabled}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label
                    htmlFor="time-range-end"
                    className="text-xs text-muted-foreground font-semibold uppercase tracking-tight"
                  >
                    End
                  </Label>
                  <Input
                    id="time-range-end"
                    type="datetime-local"
                    value={customEnd}
                    onChange={(e) => setCustomEnd(e.target.value)}
                    className="w-full"
                    disabled={disabled}
                  />
                </div>
              </div>
              <div className="flex justify-end gap-2 mt-4 pt-3 border-t border-border">
                <Button
                  htmlType="button"
                  type="quaternary"
                  theme="borderless"
                  size="small"
                  onClick={handleCustomCancel}
                >
                  Back
                </Button>
                <Button
                  htmlType="button"
                  size="small"
                  onClick={handleCustomApply}
                  disabled={!customStart || !customEnd}
                >
                  Apply
                </Button>
              </div>
            </div>
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
