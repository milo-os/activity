import type { ChangeSource } from "../types/activity";
import { Button } from "@datum-cloud/datum-ui/button";
import { cn } from "../lib/utils";

export type ChangeSourceOption = ChangeSource | "all";

export interface ChangeSourceToggleProps {
  /** Current selected value */
  value: ChangeSourceOption;
  /** Handler called when selection changes */
  onChange: (value: ChangeSourceOption) => void;
  /** Additional CSS class */
  className?: string;
  /** Whether the toggle is disabled */
  disabled?: boolean;
}

/**
 * Options for the change source toggle
 */
const OPTIONS: {
  value: ChangeSourceOption;
  label: string;
  description: string;
}[] = [
  {
    value: "all",
    label: "All",
    description: "Show all activities",
  },
  {
    value: "human",
    label: "Human",
    description: "Show only human-initiated activities",
  },
  {
    value: "system",
    label: "System",
    description: "Show only system-initiated activities",
  },
];

/**
 * ChangeSourceToggle provides a segmented control for filtering by change source
 */
export function ChangeSourceToggle({
  value,
  onChange,
  className = "",
  disabled = false,
}: ChangeSourceToggleProps) {
  return (
    <div
      className={cn(
        "inline-flex border border-input rounded-md overflow-hidden",
        className,
      )}
      role="group"
      aria-label="Filter by change source"
    >
      {OPTIONS.map((option, index) => {
        const active = value === option.value;
        return (
          <Button
            key={option.value}
            htmlType="button"
            type={active ? 'primary' : 'quaternary'}
            theme={active ? 'solid' : 'borderless'}
            className={cn(
              'px-2 h-7 text-xs font-medium transition-all duration-200',
              index < OPTIONS.length - 1 && 'border-r border-input',
              !active && 'bg-muted text-foreground hover:bg-muted/80',
            )}
            // Inline styles win over datum-ui's baked-in `rounded-lg` and
            // (for the primary/solid active button) its own `border`. The
            // outer wrapper's rounded-md + overflow-hidden draws the
            // segmented control's outer corners; per-button border-right
            // (className) draws the separators between inactive buttons.
            style={active ? { borderRadius: 0, border: 0 } : { borderRadius: 0 }}
            onClick={() => onChange(option.value)}
            disabled={disabled}
            aria-pressed={active}
            title={option.description}
          >
            {option.label}
          </Button>
        );
      })}
    </div>
  );
}
