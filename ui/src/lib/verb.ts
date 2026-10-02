import { Plus, Pencil, Trash2, Activity as ActivityIcon } from 'lucide-react';
import type { ElementType } from 'react';

/**
 * Extract verb from activity summary (e.g., "alice created HTTPProxy" -> "created")
 */
export function extractVerb(summary: string): string {
  const words = summary.split(/\s+/);
  if (words.length >= 2) {
    return words[1].toLowerCase();
  }
  return 'unknown';
}

export type NormalizedVerb = 'create' | 'update' | 'delete' | 'other';

/**
 * Normalize a verb to a canonical form used for coloring/icons/grouping.
 */
export function normalizeVerb(verb: string): NormalizedVerb {
  const normalized = verb.toLowerCase();
  if (normalized.includes('create') || normalized.includes('add')) return 'create';
  if (normalized.includes('delete') || normalized.includes('remove')) return 'delete';
  if (
    normalized.includes('update') ||
    normalized.includes('patch') ||
    normalized.includes('modify') ||
    normalized.includes('change') ||
    normalized.includes('edit')
  )
    return 'update';
  return 'other';
}

/**
 * Get icon container + icon color classes based on verb
 */
export function getActionIconClasses(verb: string): { container: string; icon: string } {
  const normalizedVerb = normalizeVerb(verb);
  switch (normalizedVerb) {
    case 'create':
      return {
        container: 'bg-blue-50 dark:bg-blue-950',
        icon: 'text-blue-500 dark:text-blue-400',
      };
    case 'update':
      return {
        container: 'bg-green-50 dark:bg-green-950',
        icon: 'text-green-600 dark:text-green-400',
      };
    case 'delete':
      return {
        container: 'bg-red-50 dark:bg-red-950',
        icon: 'text-red-500 dark:text-red-400',
      };
    default:
      return {
        container: 'bg-slate-100 dark:bg-slate-800',
        icon: 'text-slate-500 dark:text-slate-400',
      };
  }
}

/**
 * Get the Lucide icon component for the timeline node based on verb
 */
export function getTimelineIcon(verb: string): ElementType {
  const normalizedVerb = normalizeVerb(verb);
  switch (normalizedVerb) {
    case 'create':
      return Plus;
    case 'update':
      return Pencil;
    case 'delete':
      return Trash2;
    default:
      return ActivityIcon;
  }
}
