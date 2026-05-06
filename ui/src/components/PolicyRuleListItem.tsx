import type { ActivityPolicyRule } from '../types/policy';
import { Button } from '@datum-cloud/datum-ui/button';
import { Edit, Trash2, ChevronUp, ChevronDown } from 'lucide-react';

export interface PolicyRuleListItemProps {
  /** The rule being displayed */
  rule: ActivityPolicyRule;
  /** Rule type (audit or event) for context */
  ruleType: 'audit' | 'event';
  /** Rule index for display */
  index: number;
  /** Callback when edit button is clicked */
  onEdit: () => void;
  /** Callback when delete button is clicked */
  onDelete: () => void;
  /** Callback when move up button is clicked */
  onMoveUp?: () => void;
  /** Callback when move down button is clicked */
  onMoveDown?: () => void;
  /** Whether this rule can be moved up */
  canMoveUp: boolean;
  /** Whether this rule can be moved down */
  canMoveDown: boolean;
  /** Additional CSS class */
  className?: string;
}

/**
 * PolicyRuleListItem displays a single rule in a compact list format
 * with inline edit, delete, and reordering controls
 */
export function PolicyRuleListItem({
  rule,
  ruleType,
  index,
  onEdit,
  onDelete,
  onMoveUp,
  onMoveDown,
  canMoveUp,
  canMoveDown,
  className = '',
}: PolicyRuleListItemProps) {
  // Truncate match expression for display (max ~60 chars)
  const truncatedMatch = rule.match.length > 60
    ? `${rule.match.substring(0, 57)}...`
    : rule.match;

  return (
    <div
      className={`
        flex items-center gap-2 p-2 rounded-md border transition-all duration-200
        border-border bg-background hover:bg-muted/50
        ${className}
      `}
    >
      {/* Rule content */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <span className="font-medium text-xs text-foreground truncate">
            {rule.name || `${ruleType === 'audit' ? 'Audit' : 'Event'} Rule #${index + 1}`}
          </span>
        </div>

        {rule.description && (
          <div className="text-[11px] text-muted-foreground mb-0.5 truncate">
            {rule.description}
          </div>
        )}

        <div className="text-[11px] font-mono text-muted-foreground truncate" title={rule.match}>
          {truncatedMatch}
        </div>
      </div>

      {/* Action buttons */}
      <div className="flex items-center gap-1 shrink-0">
        {/* Move up/down buttons */}
        <div className="flex flex-col gap-0.5">
          <Button
            htmlType="button"
            type="quaternary" theme="borderless"
            size="icon"
            className="h-5 w-5 p-0"
            onClick={onMoveUp}
            disabled={!canMoveUp}
            title="Move up"
            aria-label="Move rule up"
          >
            <ChevronUp className="h-3.5 w-3.5" />
          </Button>
          <Button
            htmlType="button"
            type="quaternary" theme="borderless"
            size="icon"
            className="h-5 w-5 p-0"
            onClick={onMoveDown}
            disabled={!canMoveDown}
            title="Move down"
            aria-label="Move rule down"
          >
            <ChevronDown className="h-3.5 w-3.5" />
          </Button>
        </div>

        {/* Edit button */}
        <Button
          htmlType="button"
          type="quaternary" theme="borderless"
          size="icon"
          className="h-6 w-6 text-muted-foreground hover:text-foreground hover:bg-muted"
          onClick={onEdit}
          title="Edit rule"
          aria-label="Edit rule"
        >
          <Edit className="h-3.5 w-3.5" />
        </Button>

        {/* Delete button */}
        <Button
          htmlType="button"
          type="quaternary" theme="borderless"
          size="icon"
          className="h-6 w-6 text-muted-foreground hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-950/50"
          onClick={onDelete}
          title="Delete rule"
          aria-label="Delete rule"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}
