import { useState } from 'react';
import {
  AlertTriangle,
  Bell,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
} from 'lucide-react';
import type { K8sEvent } from '../types/k8s-event';
import { EventExpandedDetails } from './EventExpandedDetails';
import { cn } from '../lib/utils';
import { Button } from '@datum-cloud/datum-ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from './ui/tooltip';
import { TableCell, TableRow } from '@datum-cloud/datum-ui/table';
import { Timestamp } from './Timestamp';

// Number of columns rendered for the events table. Used by the colSpan
// on the expanded-detail row so it spans the full width.
export const EVENT_COLUMN_COUNT = 6;

export interface EventFeedItemProps {
  /** The event to render */
  event: K8sEvent;
  /** Handler called when the item is clicked */
  onEventClick?: (event: K8sEvent) => void;
  /** Handler called when the resource name is clicked. If provided, the resource name becomes clickable. */
  onResourceClick?: (resource: {
    kind: string;
    name: string;
    namespace?: string;
    uid?: string;
  }) => void;
  /** Whether the item is selected */
  isSelected?: boolean;
  /** Additional CSS class */
  className?: string;
  /** Whether to show as compact (for resource detail tabs) */
  compact?: boolean;
  /** Whether this is a newly streamed event */
  isNew?: boolean;
  /** Whether the item starts expanded */
  defaultExpanded?: boolean;
  /** Layout variant: 'feed' (table row, default) or 'timeline' (flat list row) */
  variant?: 'feed' | 'timeline';
  /** Whether this is the last item in the list (only used in timeline variant) */
  isLast?: boolean;
}

/**
 * Get the regarding object (handling both new and deprecated field names)
 */
function getRegarding(event: K8sEvent) {
  return event.regarding || event.involvedObject || {};
}

/**
 * Get the event note/message (handling both new and deprecated field names)
 */
function getNote(event: K8sEvent): string | undefined {
  return event.note || event.message;
}

/**
 * Get the reporting controller (handling both new and deprecated field names)
 */
function getReportingController(event: K8sEvent): string | undefined {
  return event.reportingController || event.source?.component;
}

/**
 * Get the event count (handling both new and deprecated field names)
 */
function getCount(event: K8sEvent): number | undefined {
  return event.series?.count || event.count || event.deprecatedCount;
}

/**
 * Get the best timestamp to display (handling both new and deprecated field names)
 * For recurring events (series), prefer lastObservedTime as it reflects the most recent occurrence.
 * For single events, use eventTime.
 */
function getTimestamp(event: K8sEvent): string | undefined {
  // For series events, lastObservedTime is the most recent occurrence
  if (event.series?.lastObservedTime) {
    return event.series.lastObservedTime;
  }
  // For single events, use eventTime (eventsv1) or fall back to deprecated/legacy fields
  // Note: events.k8s.io/v1 uses "deprecatedFirstTimestamp" and "deprecatedLastTimestamp"
  return (
    event.eventTime ||
    event.deprecatedLastTimestamp ||
    event.deprecatedFirstTimestamp ||
    event.lastTimestamp ||
    event.firstTimestamp
  );
}

/**
 * EventFeedItem renders a single Kubernetes event as a table row.
 */
export function EventFeedItem({
  event,
  onEventClick,
  onResourceClick,
  isSelected = false,
  className = '',
  compact = false,
  isNew = false,
  defaultExpanded = false,
  variant = 'feed',
  isLast = false,
}: EventFeedItemProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);
  const [isCopied, setIsCopied] = useState(false);

  // Use helper functions to handle both new and deprecated field names
  const regarding = getRegarding(event);
  const note = getNote(event);
  const count = getCount(event);
  const timestamp = getTimestamp(event);
  const reportingController = getReportingController(event);
  const { type, reason } = event;

  const handleClick = () => {
    onEventClick?.(event);
  };

  const toggleExpand = (e: React.MouseEvent) => {
    e.stopPropagation();
    setIsExpanded(!isExpanded);
  };

  const handleCopyResourceName = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (regarding.name) {
      try {
        await navigator.clipboard.writeText(regarding.name);
        setIsCopied(true);
        setTimeout(() => setIsCopied(false), 2000);
      } catch (err) {
        console.error('Failed to copy resource name:', err);
      }
    }
  };

  const handleResourceClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onResourceClick && regarding.name) {
      onResourceClick({
        kind: regarding.kind || 'Unknown',
        name: regarding.name,
        namespace: regarding.namespace,
        uid: regarding.uid,
      });
    }
  };

  const isWarning = type === 'Warning';
  const noteWithCount = note
    ? `${note}${count && count > 1 ? ` (x${count})` : ''}`
    : '';

  // Timeline variant — flat list row mirroring ActivityFeedItem timeline:
  // an icon square keyed off event type + reason/object summary +
  // timestamp + expand toggle.
  if (variant === 'timeline') {
    const TypeIcon = isWarning ? AlertTriangle : Bell;
    const iconBg = isWarning
      ? 'bg-red-50 dark:bg-red-950'
      : 'bg-blue-50 dark:bg-blue-950';
    const iconColor = isWarning
      ? 'text-red-500 dark:text-red-400'
      : 'text-blue-500 dark:text-blue-400';
    const objectLabel = regarding.namespace
      ? `${regarding.kind || 'Unknown'} · ${regarding.namespace}/${regarding.name || ''}`
      : `${regarding.kind || 'Unknown'} · ${regarding.name || ''}`;
    const summary = noteWithCount || `${reason || 'Event'} on ${regarding.name || 'unknown'}`;

    return (
      <div className={cn(!isLast && !isExpanded && 'border-b border-border', className)}>
        <div
          className={cn(
            'flex items-center gap-3 cursor-pointer group',
            compact ? 'py-2' : 'py-3',
            isSelected && 'bg-muted/40'
          )}
          onClick={toggleExpand}
        >
          {/* Type icon square */}
          <div
            className={cn(
              'w-8 h-8 rounded-md shrink-0 flex items-center justify-center',
              iconBg,
              iconColor
            )}
            title={type || 'Event'}
          >
            <TypeIcon size={16} strokeWidth={2} />
          </div>

          {/* Reason + summary text */}
          <div className="flex-1 min-w-0 text-sm leading-snug" style={{ minWidth: 0 }}>
            <div className="font-medium text-foreground truncate">
              {reason || 'Event'}
            </div>
            <Tooltip>
              <TooltipTrigger asChild>
                <div
                  className="text-xs text-muted-foreground"
                  style={{
                    display: 'block',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    cursor: 'help',
                  }}
                >
                  {summary}
                </div>
              </TooltipTrigger>
              <TooltipContent className="max-w-[480px] whitespace-normal break-words">
                {summary}
              </TooltipContent>
            </Tooltip>
          </div>

          {/* Object label */}
          <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0 max-w-[200px] truncate" title={objectLabel}>
            {regarding.name}
          </span>

          {/* Timestamp */}
          <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0">
            <Timestamp value={timestamp} />
          </span>

          {/* Expand toggle */}
          <Button
            type="quaternary"
            theme="borderless"
            size="small"
            htmlType="button"
            className="h-5 py-0 px-1 text-muted-foreground hover:text-foreground shrink-0"
            onClick={toggleExpand}
            aria-expanded={isExpanded}
          >
            {isExpanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
          </Button>
        </div>
        {isExpanded ? <EventExpandedDetails event={event} /> : null}
      </div>
    );
  }

  return (
    <>
      <TableRow
        data-state={isSelected ? 'selected' : undefined}
        className={cn(
          'cursor-pointer',
          isNew && 'bg-green-50/40 dark:bg-green-950/20',
          isWarning && !isSelected && 'border-l-2 border-l-red-400',
          className
        )}
        onClick={(e) => {
          toggleExpand(e);
          handleClick();
        }}
        aria-expanded={isExpanded}
      >
        <TableCell className="py-2 align-middle whitespace-nowrap">
          <span
            className={cn(
              'inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium',
              isWarning
                ? 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300'
                : 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300'
            )}
          >
            {type || 'Unknown'}
          </span>
        </TableCell>
        <TableCell className="py-2 align-middle whitespace-nowrap text-sm font-medium">
          {reason || ''}
        </TableCell>
        <TableCell
          className="py-2 align-middle"
          style={{ width: '100%', maxWidth: 0, overflow: 'hidden' }}
        >
          {note ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <div
                  className="text-sm text-muted-foreground leading-snug"
                  style={{
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {noteWithCount}
                </div>
              </TooltipTrigger>
              <TooltipContent className="max-w-[480px] whitespace-normal break-words">
                {noteWithCount}
              </TooltipContent>
            </Tooltip>
          ) : null}
        </TableCell>
        <TableCell className="py-2 align-middle">
          <div className="flex items-center gap-1 min-w-0">
            <Tooltip>
              <TooltipTrigger asChild>
                <span
                  className={cn(
                    'text-sm font-medium text-foreground truncate',
                    onResourceClick && 'cursor-pointer hover:underline hover:text-primary'
                  )}
                  onClick={onResourceClick ? handleResourceClick : undefined}
                  style={{ maxWidth: 200 }}
                >
                  {regarding.name || 'Unknown'}
                </span>
              </TooltipTrigger>
              <TooltipContent>
                {regarding.namespace
                  ? `${regarding.kind || 'Unknown'} · ${regarding.namespace}/${regarding.name}`
                  : `${regarding.kind || 'Unknown'} · ${regarding.name}`}
              </TooltipContent>
            </Tooltip>
            <button
              onClick={handleCopyResourceName}
              className="inline-flex items-center justify-center p-0.5 rounded opacity-0 group-hover:opacity-100 hover:bg-gray-100 dark:hover:bg-gray-800 cursor-pointer"
              aria-label="Copy resource name"
              type="button"
            >
              {isCopied ? (
                <Check className="h-3 w-3 text-green-600 dark:text-green-400" />
              ) : (
                <Copy className="h-3 w-3 text-gray-500 dark:text-gray-400" />
              )}
            </button>
          </div>
        </TableCell>
        <TableCell className="py-2 align-middle whitespace-nowrap text-sm text-muted-foreground">
          <Timestamp value={timestamp} />
        </TableCell>
        <TableCell className="py-2 align-middle w-10">
          <Button
            type="quaternary" theme="borderless"
            size="small"
            htmlType="button"
            className="h-6 w-6 p-0 text-muted-foreground hover:text-foreground"
            onClick={toggleExpand}
            aria-expanded={isExpanded}
            aria-label={isExpanded ? 'Collapse details' : 'Expand details'}
          >
            {isExpanded ? (
              <ChevronDown className="h-4 w-4" />
            ) : (
              <ChevronRight className="h-4 w-4" />
            )}
          </Button>
        </TableCell>
      </TableRow>
      {isExpanded ? (
        <TableRow className="bg-muted/30 hover:bg-muted/30">
          <TableCell colSpan={EVENT_COLUMN_COUNT} className="p-0">
            <EventExpandedDetails event={event} />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
