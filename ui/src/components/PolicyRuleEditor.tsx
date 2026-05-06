import { useState } from 'react';
import type { ActivityPolicyRule } from '../types/policy';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardContent, CardHeader } from '@datum-cloud/datum-ui/card';
import { Label } from '@datum-cloud/datum-ui/label';
import { Textarea } from '@datum-cloud/datum-ui/textarea';

export interface PolicyRuleEditorProps {
  /** The rule being edited */
  rule: ActivityPolicyRule;
  /** Rule index for display */
  index: number;
  /** Rule type (audit or event) for context-sensitive help */
  ruleType: 'audit' | 'event';
  /** Whether this rule is highlighted (e.g., matched in preview) */
  isHighlighted?: boolean;
  /** Callback when rule changes */
  onChange: (rule: ActivityPolicyRule) => void;
  /** Callback to delete this rule */
  onDelete: () => void;
  /** Additional CSS class */
  className?: string;
}

/**
 * CEL syntax help for audit rules
 */
const AUDIT_CEL_HELP = {
  variables: [
    { name: 'audit.verb', description: 'HTTP verb (create, update, patch, delete, get, list, watch)' },
    { name: 'audit.user.username', description: 'Username of the actor' },
    { name: 'audit.user.groups', description: 'Groups the actor belongs to' },
    { name: 'audit.objectRef.name', description: 'Name of the resource' },
    { name: 'audit.objectRef.namespace', description: 'Namespace of the resource' },
    { name: 'audit.objectRef.subresource', description: 'Subresource (e.g., "status")' },
    { name: 'audit.responseStatus.code', description: 'HTTP response status code' },
  ],
  examples: [
    'audit.verb == "create"',
    'audit.verb in ["create", "update", "patch"]',
    'audit.verb == "update" && audit.objectRef.subresource == "status"',
    'audit.responseStatus.code >= 200 && audit.responseStatus.code < 300',
  ],
};

/**
 * CEL syntax help for event rules
 */
const EVENT_CEL_HELP = {
  variables: [
    { name: 'event.type', description: 'Event type: "Normal" or "Warning"' },
    { name: 'event.reason', description: 'Short reason (e.g., "Created", "Ready", "Failed")' },
    { name: 'event.note', description: 'Human-readable description of the event' },
    { name: 'event.regarding.name', description: 'Name of the regarding resource' },
    { name: 'event.regarding.namespace', description: 'Namespace of the regarding resource' },
    { name: 'event.reportingController', description: 'Controller that emitted the event' },
  ],
  examples: [
    'event.reason == "Ready"',
    'event.type == "Warning"',
    'event.reason in ["Created", "Updated", "Deleted"]',
    'event.note.contains("failed")',
  ],
};

/**
 * Summary template help
 */
const SUMMARY_HELP = {
  variables: [
    { name: 'actor.name', description: 'Display name of the actor' },
    { name: 'actor.email', description: 'Email of the actor (if available)' },
    { name: 'resource.name', description: 'Name of the resource' },
    { name: 'resource.namespace', description: 'Namespace of the resource' },
    { name: 'kindLabel', description: 'Human-readable kind label (e.g., "HTTP Proxy")' },
    { name: 'kindLabelPlural', description: 'Plural kind label (e.g., "HTTP Proxies")' },
  ],
  examples: [
    '{{ actor.name }} created {{ kindLabel }} {{ resource.name }}',
    '{{ actor.name }} updated the status of {{ kindLabel }} {{ resource.name }}',
    '{{ kindLabel }} {{ resource.name }} is now ready',
    '{{ kindLabel }} {{ resource.name }} failed: {{ event.note }}',
  ],
};

/**
 * PolicyRuleEditor provides editing UI for a single policy rule
 */
export function PolicyRuleEditor({
  rule,
  index,
  ruleType,
  isHighlighted = false,
  onChange,
  onDelete,
  className = '',
}: PolicyRuleEditorProps) {
  const [showMatchHelp, setShowMatchHelp] = useState(false);
  const [showSummaryHelp, setShowSummaryHelp] = useState(false);

  const celHelp = ruleType === 'audit' ? AUDIT_CEL_HELP : EVENT_CEL_HELP;

  const handleMatchChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    onChange({ ...rule, match: e.target.value });
  };

  const handleSummaryChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    onChange({ ...rule, summary: e.target.value });
  };

  const insertMatchExample = (example: string) => {
    onChange({ ...rule, match: example });
  };

  const insertSummaryExample = (example: string) => {
    onChange({ ...rule, summary: example });
  };

  return (
    <Card
      className={`mb-3 transition-all duration-200 ${
        isHighlighted
          ? 'border-emerald-600 bg-emerald-50'
          : 'bg-muted'
      } ${className}`}
    >
      <CardHeader className="flex flex-row justify-between items-center p-3 pb-0">
        <span
          className={`text-xs font-semibold uppercase tracking-wide ${
            isHighlighted ? 'text-emerald-600' : 'text-muted-foreground'
          }`}
        >
          {ruleType === 'audit' ? 'Audit' : 'Event'} Rule #{index + 1}
        </span>
        <Button
          htmlType="button"
          type="quaternary" theme="borderless"
          size="icon"
          className="w-6 h-6 text-xl leading-none text-muted-foreground hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-950/50 dark:hover:text-red-400"
          onClick={onDelete}
          title="Delete rule"
        >
          ×
        </Button>
      </CardHeader>

      <CardContent className="p-3">
        {/* Match Expression */}
        <div className="mb-3">
          <div className="flex justify-between items-center mb-1">
            <Label htmlFor={`rule-${index}-match`}>
              Match Expression (CEL)
            </Label>
            <Button
              htmlType="button"
              type="tertiary" theme="outline"
              size="small"
              className="px-2 py-0.5 h-auto text-xs"
              onClick={() => setShowMatchHelp(!showMatchHelp)}
            >
              {showMatchHelp ? 'Hide Help' : 'Show Help'}
            </Button>
          </div>
          <Textarea
            id={`rule-${index}-match`}
            className="font-mono text-sm resize-y min-h-[60px]"
            value={rule.match}
            onChange={handleMatchChange}
            placeholder={`e.g., ${celHelp.examples[0]}`}
            rows={2}
            spellCheck={false}
          />
          {showMatchHelp && (
            <div className="mt-2 p-3 bg-background border border-border rounded-md text-xs">
              <div className="mb-3 last:mb-0">
                <strong className="block mb-1.5 text-foreground">Available Variables:</strong>
                <ul className="m-0 pl-5 list-disc">
                  {celHelp.variables.map((v) => (
                    <li key={v.name} className="mb-0.5">
                      <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{v.name}</code> - {v.description}
                    </li>
                  ))}
                </ul>
              </div>
              <div className="mb-3 last:mb-0">
                <strong className="block mb-1.5 text-foreground">Examples:</strong>
                <ul className="m-0 p-0 list-none">
                  {celHelp.examples.map((ex, i) => (
                    <li key={i} className="flex items-center gap-2 mb-1 px-2 py-1 bg-muted rounded">
                      <code className="flex-1 text-xs break-all">{ex}</code>
                      <Button
                        htmlType="button"
                        size="small"
                        className="px-2 py-0.5 h-auto bg-primary text-primary-foreground border-none text-xs font-medium uppercase hover:bg-primary/90"
                        onClick={() => insertMatchExample(ex)}
                      >
                        Use
                      </Button>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          )}
        </div>

        {/* Summary Template */}
        <div className="mb-3 last:mb-0">
          <div className="flex justify-between items-center mb-1">
            <Label htmlFor={`rule-${index}-summary`}>
              Summary Template (CEL)
            </Label>
            <Button
              htmlType="button"
              type="tertiary" theme="outline"
              size="small"
              className="px-2 py-0.5 h-auto text-xs"
              onClick={() => setShowSummaryHelp(!showSummaryHelp)}
            >
              {showSummaryHelp ? 'Hide Help' : 'Show Help'}
            </Button>
          </div>
          <Textarea
            id={`rule-${index}-summary`}
            className="font-mono text-sm resize-y min-h-[60px]"
            value={rule.summary}
            onChange={handleSummaryChange}
            placeholder={`e.g., ${SUMMARY_HELP.examples[0]}`}
            rows={2}
            spellCheck={false}
          />
          {showSummaryHelp && (
            <div className="mt-2 p-3 bg-background border border-border rounded-md text-xs">
              <div className="mb-3 last:mb-0">
                <strong className="block mb-1.5 text-foreground">Available Variables:</strong>
                <ul className="m-0 pl-5 list-disc">
                  {SUMMARY_HELP.variables.map((v) => (
                    <li key={v.name} className="mb-0.5">
                      <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{'{{ ' + v.name + ' }}'}</code> - {v.description}
                    </li>
                  ))}
                </ul>
              </div>
              <div className="mb-3 last:mb-0">
                <strong className="block mb-1.5 text-foreground">Examples:</strong>
                <ul className="m-0 p-0 list-none">
                  {SUMMARY_HELP.examples.map((ex, i) => (
                    <li key={i} className="flex items-center gap-2 mb-1 px-2 py-1 bg-muted rounded">
                      <code className="flex-1 text-xs break-all">{ex}</code>
                      <Button
                        htmlType="button"
                        size="small"
                        className="px-2 py-0.5 h-auto bg-primary text-primary-foreground border-none text-xs font-medium uppercase hover:bg-primary/90"
                        onClick={() => insertSummaryExample(ex)}
                      >
                        Use
                      </Button>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
