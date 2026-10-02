import type { ChangeSource } from "../types/activity";
import { SegmentedToggle, type SegmentedToggleOption } from "./ui/segmented-toggle";

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
const OPTIONS: SegmentedToggleOption<ChangeSourceOption>[] = [
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
  className,
  disabled = false,
}: ChangeSourceToggleProps) {
  return (
    <SegmentedToggle
      options={OPTIONS}
      value={value}
      onChange={onChange}
      ariaLabel="Filter by change source"
      className={className}
      disabled={disabled}
    />
  );
}
