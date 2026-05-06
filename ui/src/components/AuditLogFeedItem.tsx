import { useState } from 'react';
import { Activity as ActivityIcon, Pencil, Plus, Trash2 } from 'lucide-react';
import type { Event } from '../types';
import { AuditLogExpandedDetails } from './AuditLogExpandedDetails';
import { cn } from '../lib/utils';
import { Button } from '@datum-cloud/datum-ui/button';
import { Badge } from './ui/badge';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';
import { TableCell, TableRow } from '@datum-cloud/datum-ui/table';
import { Timestamp } from './Timestamp';

// Number of columns rendered for the audit log table. Used by the colSpan
// on the expanded-detail row so it spans the full width.
export const AUDIT_LOG_COLUMN_COUNT = 5;

export interface AuditLogFeedItemProps {
  /** The audit event to render */
  event: Event;
  /** Handler called when the item is clicked */
  onEventClick?: (event: Event) => void;
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
  /** Whether this is the last item (only used in timeline variant) */
  isLast?: boolean;
}

/**
 * Get Tailwind classes for verb badge
 */
function getVerbBadgeClasses(verb?: string): string {
  const baseClasses = 'text-[0.55rem] h-5 px-2 py-1 leading-3';
  const normalized = verb?.toLowerCase();

  switch (normalized) {
    case 'create':
      return cn(baseClasses, 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300');
    case 'update':
    case 'patch':
      return cn(baseClasses, 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300');
    case 'delete':
      return cn(baseClasses, 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300');
    default:
      return cn(baseClasses, 'bg-muted text-muted-foreground');
  }
}

/**
 * Get response status indicator (✓ or ✗)
 */
function getResponseStatusIndicator(code?: number): { icon: string; className: string } {
  if (!code) {
    return { icon: '?', className: 'text-muted-foreground' };
  }

  if (code >= 200 && code < 300) {
    return { icon: '✓', className: 'text-green-600 dark:text-green-400' };
  }

  return { icon: '✗', className: 'text-red-600 dark:text-red-400' };
}

/**
 * Build human-readable summary
 */
function buildAuditSummary(event: Event): string {
  const username = event.user?.username || 'Unknown user';
  const verb = event.verb || 'performed action';
  const kind = event.objectRef?.resource || 'resource';
  const name = event.objectRef?.name || '';
  const namespace = event.objectRef?.namespace;

  let summary = `${username} ${verb} ${kind}`;
  if (name) {
    summary += ` ${name}`;
  }
  if (namespace) {
    summary += ` in ${namespace}`;
  }

  return summary;
}

/**
 * AuditLogFeedItem renders a single audit log event in the feed
 */
export function AuditLogFeedItem({
  event,
  onEventClick,
  isSelected = false,
  className = '',
  compact = false,
  isNew = false,
  defaultExpanded = false,
  variant = 'feed',
  isLast = false,
}: AuditLogFeedItemProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const handleClick = () => {
    onEventClick?.(event);
  };

  const toggleExpand = (e: React.MouseEvent) => {
    e.stopPropagation();
    setIsExpanded(!isExpanded);
  };

  const timestamp = event.stageTimestamp || event.requestReceivedTimestamp;
  const summary = buildAuditSummary(event);
  const statusIndicator = getResponseStatusIndicator(event.responseStatus?.code);

  if (variant === 'timeline') {
    const verb = event.verb?.toLowerCase() || '';
    const Icon =
      verb === 'create' ? Plus :
      verb === 'delete' ? Trash2 :
      verb === 'update' || verb === 'patch' ? Pencil :
      ActivityIcon;
    const iconBg =
      verb === 'create' ? 'bg-green-50 dark:bg-green-950' :
      verb === 'delete' ? 'bg-red-50 dark:bg-red-950' :
      verb === 'update' || verb === 'patch' ? 'bg-amber-50 dark:bg-amber-950' :
      'bg-slate-100 dark:bg-slate-800';
    const iconColor =
      verb === 'create' ? 'text-green-600 dark:text-green-400' :
      verb === 'delete' ? 'text-red-500 dark:text-red-400' :
      verb === 'update' || verb === 'patch' ? 'text-amber-600 dark:text-amber-400' :
      'text-slate-500 dark:text-slate-400';

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
          <div
            className={cn(
              'w-8 h-8 rounded-md shrink-0 flex items-center justify-center',
              iconBg,
              iconColor
            )}
            title={event.verb}
          >
            <Icon size={16} strokeWidth={2} />
          </div>
          <div className="flex-1 min-w-0 text-sm leading-snug" style={{ minWidth: 0 }}>
            <Tooltip>
              <TooltipTrigger asChild>
                <div
                  className="text-foreground"
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
          <span
            className={cn(
              'inline-flex items-center gap-1 text-xs font-semibold shrink-0',
              statusIndicator.className
            )}
          >
            <span>{statusIndicator.icon}</span>
            {event.responseStatus?.code ? <span>{event.responseStatus.code}</span> : null}
          </span>
          <span className="text-xs text-muted-foreground whitespace-nowrap shrink-0">
            <Timestamp value={timestamp} />
          </span>
          <Button
            type="quaternary"
            theme="borderless"
            size="small"
            htmlType="button"
            className="h-5 py-0 px-1 text-base text-muted-foreground hover:text-foreground shrink-0"
            onClick={toggleExpand}
            aria-expanded={isExpanded}
            aria-label={isExpanded ? 'Collapse details' : 'Expand details'}
          >
            {isExpanded ? '−' : '+'}
          </Button>
        </div>
        {isExpanded ? <AuditLogExpandedDetails event={event} /> : null}
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
          className
        )}
        onClick={(e) => {
          toggleExpand(e);
          handleClick();
        }}
        aria-expanded={isExpanded}
      >
        <TableCell className="py-2 align-middle whitespace-nowrap">
          <Badge className={getVerbBadgeClasses(event.verb)}>
            {event.verb?.toUpperCase() || 'UNKNOWN'}
          </Badge>
        </TableCell>
        <TableCell
          className="py-2 align-middle"
          style={{ width: '100%', maxWidth: 0, overflow: 'hidden' }}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <div
                className="text-sm leading-snug"
                style={{
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {summary}
              </div>
            </TooltipTrigger>
            <TooltipContent className="max-w-[480px] whitespace-normal break-words">
              {summary}
            </TooltipContent>
          </Tooltip>
        </TableCell>
        <TableCell className="py-2 align-middle whitespace-nowrap">
          <span
            className={cn(
              'inline-flex items-center gap-1 text-xs font-semibold',
              statusIndicator.className
            )}
          >
            <span>{statusIndicator.icon}</span>
            {event.responseStatus?.code ? <span>{event.responseStatus.code}</span> : null}
          </span>
        </TableCell>
        <TableCell className="py-2 align-middle whitespace-nowrap text-sm text-muted-foreground">
          <Timestamp value={timestamp} />
        </TableCell>
        <TableCell className="py-2 align-middle w-10">
          <Button
            type="quaternary" theme="borderless"
            size="small"
            htmlType="button"
            className="h-6 w-6 p-0 text-base text-muted-foreground hover:text-foreground"
            onClick={toggleExpand}
            aria-expanded={isExpanded}
            aria-label={isExpanded ? 'Collapse details' : 'Expand details'}
          >
            {isExpanded ? '−' : '+'}
          </Button>
        </TableCell>
      </TableRow>
      {isExpanded ? (
        <TableRow className="bg-muted/30 hover:bg-muted/30">
          <TableCell colSpan={AUDIT_LOG_COLUMN_COUNT} className="p-0">
            <AuditLogExpandedDetails event={event} />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
