import { formatDistanceToNowStrict } from 'date-fns';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

export interface TimestampProps {
  /** ISO string or Date. */
  value?: string | Date;
  /**
   * Visible variant.
   * - `relative` (default): "11 hours ago"
   * - `utc`: "01 May 26 10:30:50"
   * - `local`: same format in browser timezone
   * - `iso-utc`: "2026-04-30 23:34:36 UTC" — verbose for detail panels
   */
  variant?: 'relative' | 'utc' | 'local' | 'iso-utc';
  /** Optional className applied to the visible span. */
  className?: string;
}

/**
 * Format a Date in UTC, e.g. "01 May 26 10:30:50".
 */
function formatUTC(date: Date): string {
  return formatInTimeZone(date, 'UTC');
}

/**
 * Verbose ISO-style UTC, e.g. "2026-04-30 23:34:36 UTC". Used in detail
 * panels where space allows the unambiguous form.
 */
function formatISOUTC(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())} ` +
    `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}:${pad(date.getUTCSeconds())} UTC`
  );
}

/**
 * Format a Date in the browser's local timezone, e.g. "01 May 26 03:30:50".
 */
function formatLocal(date: Date): string {
  return formatInTimeZone(date, getBrowserTimezone());
}

/**
 * Format a Date in the given IANA timezone using Intl, matching the
 * 'dd MMM yy HH:mm:ss' shape used elsewhere in the design system.
 */
function formatInTimeZone(date: Date, timeZone: string): string {
  const fmt = new Intl.DateTimeFormat('en-GB', {
    timeZone,
    day: '2-digit',
    month: 'short',
    year: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  // 'en-GB' produces e.g. "01 May 26, 10:30:50" — strip the comma so it
  // reads "01 May 26 10:30:50".
  return fmt.format(date).replace(',', '');
}

/**
 * Best-effort browser timezone, falls back to UTC if Intl is unavailable.
 */
function getBrowserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

/**
 * Microsecond Unix timestamp as a string. Microseconds chosen to match
 * how observability tooling (e.g. Loki, Tempo) expresses timestamps.
 */
function getMicrosecondTimestamp(date: Date): string {
  return (date.getTime() * 1000).toString();
}

/**
 * Hover tooltip body with UTC / local / relative / microsecond timestamp.
 */
function TimestampTooltipBody({ date }: { date: Date }) {
  const tz = getBrowserTimezone();
  const rows: Array<[string, string]> = [
    ['UTC', formatUTC(date)],
    [tz.replace(/_/g, ' '), formatInTimeZone(date, tz)],
    ['Relative', formatDistanceToNowStrict(date, { addSuffix: true })],
    ['Timestamp', getMicrosecondTimestamp(date)],
  ];
  return (
    <div className="flex flex-col gap-1.5 text-xs">
      {rows.map(([label, value]) => (
        <div key={label} className="flex items-baseline gap-3">
          <span className="font-medium shrink-0">{label}</span>
          <span className="font-mono opacity-80 break-all" style={{ overflowWrap: 'anywhere' }}>
            {value}
          </span>
        </div>
      ))}
    </div>
  );
}

/**
 * Timestamp displays a date in one of three visible variants and shows a
 * detailed tooltip on hover with UTC, browser-local, relative, and
 * microsecond-precision timestamp formats.
 */
export function Timestamp({ value, variant = 'relative', className }: TimestampProps) {
  if (!value) return null;
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return null;

  let visible: string;
  switch (variant) {
    case 'utc':
      visible = formatUTC(date);
      break;
    case 'iso-utc':
      visible = formatISOUTC(date);
      break;
    case 'local':
      visible = formatLocal(date);
      break;
    case 'relative':
    default:
      visible = formatDistanceToNowStrict(date, { addSuffix: true });
      break;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={className} style={{ cursor: 'help' }}>
          {visible}
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <TimestampTooltipBody date={date} />
      </TooltipContent>
    </Tooltip>
  );
}
