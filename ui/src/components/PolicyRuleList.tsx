import { useState } from 'react';
import type { ActivityPolicyRule } from '../types/policy';
import type { ActivityApiClient } from '../api/client';
import { PolicyRuleListItem } from './PolicyRuleListItem';
import { PolicyRuleEditorDialog } from './PolicyRuleEditorDialog';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@datum-cloud/datum-ui/tabs';
import { Button } from '@datum-cloud/datum-ui/button';
import { Badge } from './ui/badge';

export interface PolicyRuleListProps {
  /** Audit rules */
  auditRules: ActivityPolicyRule[];
  /** Event rules */
  eventRules: ActivityPolicyRule[];
  /** Policy resource configuration */
  policyResource: { apiGroup: string; kind: string };
  /** Activity API client for preview */
  apiClient: ActivityApiClient;
  /** Callback when audit rules change */
  onAuditRulesChange: (rules: ActivityPolicyRule[]) => void;
  /** Callback when event rules change */
  onEventRulesChange: (rules: ActivityPolicyRule[]) => void;
  /** Additional CSS class */
  className?: string;
}

/**
 * PolicyRuleList displays and manages both audit and event rules with tabs
 */
export function PolicyRuleList({
  auditRules,
  eventRules,
  policyResource,
  apiClient,
  onAuditRulesChange,
  onEventRulesChange,
  className = '',
}: PolicyRuleListProps) {
  // Dialog state
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<ActivityPolicyRule | null>(null);
  const [editingRuleIndex, setEditingRuleIndex] = useState<number | null>(null);
  const [editingRuleType, setEditingRuleType] = useState<'audit' | 'event'>('audit');

  // Open dialog for creating new rule
  const handleAddRule = (ruleType: 'audit' | 'event') => {
    setEditingRule(null);
    setEditingRuleIndex(null);
    setEditingRuleType(ruleType);
    setDialogOpen(true);
  };

  // Open dialog for editing existing rule
  const handleEditRule = (index: number, ruleType: 'audit' | 'event') => {
    const rules = ruleType === 'audit' ? auditRules : eventRules;
    setEditingRule(rules[index]);
    setEditingRuleIndex(index);
    setEditingRuleType(ruleType);
    setDialogOpen(true);
  };

  // Save rule (create or update)
  const handleSaveRule = (rule: ActivityPolicyRule) => {
    if (editingRuleType === 'audit') {
      if (editingRuleIndex === null) {
        // Add new audit rule
        onAuditRulesChange([...auditRules, rule]);
      } else {
        // Update existing audit rule
        const newRules = [...auditRules];
        newRules[editingRuleIndex] = rule;
        onAuditRulesChange(newRules);
      }
    } else {
      if (editingRuleIndex === null) {
        // Add new event rule
        onEventRulesChange([...eventRules, rule]);
      } else {
        // Update existing event rule
        const newRules = [...eventRules];
        newRules[editingRuleIndex] = rule;
        onEventRulesChange(newRules);
      }
    }
  };

  // Delete rule
  const handleDeleteRule = (index: number, ruleType: 'audit' | 'event') => {
    if (ruleType === 'audit') {
      const newRules = auditRules.filter((_, i) => i !== index);
      onAuditRulesChange(newRules);
    } else {
      const newRules = eventRules.filter((_, i) => i !== index);
      onEventRulesChange(newRules);
    }
  };

  // Move rule up
  const handleMoveUp = (index: number, ruleType: 'audit' | 'event') => {
    if (index === 0) return;

    const rules = ruleType === 'audit' ? [...auditRules] : [...eventRules];
    [rules[index - 1], rules[index]] = [rules[index], rules[index - 1]];

    if (ruleType === 'audit') {
      onAuditRulesChange(rules);
    } else {
      onEventRulesChange(rules);
    }
  };

  // Move rule down
  const handleMoveDown = (index: number, ruleType: 'audit' | 'event') => {
    const rules = ruleType === 'audit' ? auditRules : eventRules;
    if (index >= rules.length - 1) return;

    const newRules = [...rules];
    [newRules[index], newRules[index + 1]] = [newRules[index + 1], newRules[index]];

    if (ruleType === 'audit') {
      onAuditRulesChange(newRules);
    } else {
      onEventRulesChange(newRules);
    }
  };

  // Get existing rule names for validation
  const existingNames = editingRuleType === 'audit'
    ? auditRules.map(r => r.name || '').filter(Boolean)
    : eventRules.map(r => r.name || '').filter(Boolean);

  return (
    <>
      <div className={`bg-muted rounded-lg overflow-hidden ${className}`}>
        <Tabs defaultValue="audit" className="w-full">
          <TabsList className="w-full rounded-none border-b border-input bg-muted h-auto p-0">
            <TabsTrigger
              value="audit"
              className="flex-1 gap-1.5 py-1.5 px-2 text-xs rounded-none data-[state=active]:bg-background data-[state=active]:border-b-2 data-[state=active]:border-[#BF9595] data-[state=active]:shadow-none"
            >
              Audit Rules
              <Badge
                variant="secondary"
                className="text-[10px] px-1.5 py-0 data-[state=active]:bg-primary data-[state=active]:text-primary-foreground"
              >
                {auditRules.length}
              </Badge>
            </TabsTrigger>
            <TabsTrigger
              value="event"
              className="flex-1 gap-1.5 py-1.5 px-2 text-xs rounded-none data-[state=active]:bg-background data-[state=active]:border-b-2 data-[state=active]:border-[#BF9595] data-[state=active]:shadow-none"
            >
              Event Rules
              <Badge
                variant="secondary"
                className="text-[10px] px-1.5 py-0 data-[state=active]:bg-primary data-[state=active]:text-primary-foreground"
              >
                {eventRules.length}
              </Badge>
            </TabsTrigger>
          </TabsList>

          <TabsContent value="audit" className="mt-0 p-2 bg-background">
            <div className="space-y-1.5">
              {auditRules.map((rule, index) => (
                <PolicyRuleListItem
                  key={rule.name}
                  rule={rule}
                  ruleType="audit"
                  index={index}
                  onEdit={() => handleEditRule(index, 'audit')}
                  onDelete={() => handleDeleteRule(index, 'audit')}
                  onMoveUp={() => handleMoveUp(index, 'audit')}
                  onMoveDown={() => handleMoveDown(index, 'audit')}
                  canMoveUp={index > 0}
                  canMoveDown={index < auditRules.length - 1}
                />
              ))}
            </div>
            {auditRules.length === 0 && (
              <div className="text-center py-4 px-4 text-muted-foreground text-xs">
                <p className="mb-1">No audit rules defined.</p>
                <p className="text-[11px]">
                  Audit rules match API audit events and generate activity summaries.
                </p>
              </div>
            )}
            <Button
              htmlType="button"
              type="tertiary" theme="outline"
              size="small"
              className="w-full mt-2 border-2 border-dashed border-input bg-muted hover:bg-accent hover:border-primary hover:text-foreground text-xs"
              onClick={() => handleAddRule('audit')}
            >
              + Add Audit Rule
            </Button>
          </TabsContent>

          <TabsContent value="event" className="mt-0 p-2 bg-background">
            <div className="space-y-1.5">
              {eventRules.map((rule, index) => (
                <PolicyRuleListItem
                  key={rule.name}
                  rule={rule}
                  ruleType="event"
                  index={index}
                  onEdit={() => handleEditRule(index, 'event')}
                  onDelete={() => handleDeleteRule(index, 'event')}
                  onMoveUp={() => handleMoveUp(index, 'event')}
                  onMoveDown={() => handleMoveDown(index, 'event')}
                  canMoveUp={index > 0}
                  canMoveDown={index < eventRules.length - 1}
                />
              ))}
            </div>
            {eventRules.length === 0 && (
              <div className="text-center py-4 px-4 text-muted-foreground text-xs">
                <p className="mb-1">No event rules defined.</p>
                <p className="text-[11px]">
                  Event rules match Kubernetes Events and generate activity summaries.
                </p>
              </div>
            )}
            <Button
              htmlType="button"
              type="tertiary" theme="outline"
              size="small"
              className="w-full mt-2 border-2 border-dashed border-input bg-muted hover:bg-accent hover:border-primary hover:text-foreground text-xs"
              onClick={() => handleAddRule('event')}
            >
              + Add Event Rule
            </Button>
          </TabsContent>
        </Tabs>
      </div>

      {/* Rule Editor Dialog */}
      <PolicyRuleEditorDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        rule={editingRule}
        ruleType={editingRuleType}
        existingNames={existingNames}
        policyResource={policyResource}
        apiClient={apiClient}
        onSave={handleSaveRule}
        onCancel={() => setDialogOpen(false)}
      />
    </>
  );
}
