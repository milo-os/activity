import { useState, useEffect, useMemo } from 'react';
import type { ActivityPolicyRule, PolicyPreviewStatus } from '../types/policy';
import type { ActivityApiClient } from '../api/client';
import { Button } from '@datum-cloud/datum-ui/button';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@datum-cloud/datum-ui/sheet';
import { Input } from '@datum-cloud/datum-ui/input';
import { Label } from '@datum-cloud/datum-ui/label';
import { Textarea } from '@datum-cloud/datum-ui/textarea';
import { PolicyPreviewPanel } from './PolicyPreviewPanel';
import { CelEditor } from './CelEditor';
import { extractFieldPathsFromMany } from '../lib/extractFieldPaths';

export interface PolicyRuleEditorDialogProps {
  /** Whether the dialog is open */
  open: boolean;
  /** Callback when open state changes */
  onOpenChange: (open: boolean) => void;
  /** Rule being edited (null for creating new rule) */
  rule: ActivityPolicyRule | null;
  /** Rule type (audit or event) */
  ruleType: 'audit' | 'event';
  /** Existing rule names for uniqueness validation */
  existingNames: string[];
  /** Policy resource configuration */
  policyResource: { apiGroup: string; kind: string };
  /** Activity API client for preview */
  apiClient: ActivityApiClient;
  /** Callback when rule is saved */
  onSave: (rule: ActivityPolicyRule) => void;
  /** Callback when canceled */
  onCancel: () => void;
}

/**
 * CEL syntax help for audit rules
 */
const AUDIT_CEL_HELP = {
  variables: [
    'audit.verb - HTTP verb (create, update, patch, delete, get, list, watch)',
    'audit.user.username - Username of the actor',
    'audit.user.groups - Groups the actor belongs to',
    'audit.objectRef.name - Name of the resource',
    'audit.objectRef.namespace - Namespace of the resource',
    'audit.objectRef.subresource - Subresource (e.g., "status")',
    'audit.responseStatus.code - HTTP response status code',
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
    'event.type - Event type: "Normal" or "Warning"',
    'event.reason - Short reason (e.g., "Created", "Ready", "Failed")',
    'event.note - Human-readable description of the event',
    'event.regarding.name - Name of the regarding resource',
    'event.regarding.namespace - Namespace of the regarding resource',
    'event.reportingController - Controller that emitted the event',
  ],
  examples: [
    'event.reason == "Ready"',
    'event.type == "Warning"',
    'event.reason in ["Created", "Updated", "Deleted"]',
    'event.note.contains("failed")',
  ],
};

/**
 * Summary template help for audit rules
 */
const AUDIT_SUMMARY_HELP = {
  variables: [
    '{{ actor }} - Display name of the actor',
    '{{ kind }} - Resource kind (e.g., "HTTPProxy")',
    '{{ verb }} - Action verb (create, update, delete, etc.)',
    '{{ objectRef.name }} - Name of the resource',
    '{{ objectRef.namespace }} - Namespace of the resource',
    '{{ link(text, ref) }} - Create a clickable link',
  ],
  examples: [
    '{{ actor }} created {{ kind }} {{ objectRef.name }}',
    '{{ actor }} {{ verb }}d {{ link(kind + " " + objectRef.name, objectRef) }}',
    '{{ actor }} updated the status of {{ kind }} {{ objectRef.name }}',
  ],
};

/**
 * Summary template help for event rules
 */
const EVENT_SUMMARY_HELP = {
  variables: [
    '{{ actor }} - Display name of the actor/controller',
    '{{ event.reason }} - Event reason (e.g., "Ready", "Failed")',
    '{{ event.note }} - Human-readable event message',
    '{{ event.regarding.name }} - Name of the regarding resource',
    '{{ event.regarding.namespace }} - Namespace of the regarding resource',
    '{{ link(text, ref) }} - Create a clickable link',
  ],
  examples: [
    '{{ link(event.regarding.kind + " " + event.regarding.name, event.regarding) }} is now ready',
    '{{ event.regarding.kind }} {{ event.regarding.name }} failed: {{ event.note }}',
    '{{ actor }} reported {{ event.reason }} for {{ event.regarding.name }}',
  ],
};

/**
 * Validate DNS subdomain name (RFC 1123)
 * Must consist of lowercase alphanumeric characters or '-',
 * and must start and end with an alphanumeric character
 */
function isValidDNSSubdomain(name: string): boolean {
  if (!name || name.length === 0 || name.length > 253) {
    return false;
  }
  // Fixed pattern that allows single-character names
  const pattern = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
  return pattern.test(name);
}

/**
 * PolicyRuleEditorDialog provides a dialog interface for editing policy rules
 */
export function PolicyRuleEditorDialog({
  open,
  onOpenChange,
  rule,
  ruleType,
  existingNames,
  policyResource,
  apiClient,
  onSave,
  onCancel,
}: PolicyRuleEditorDialogProps) {
  const isCreating = rule === null;

  // Form state
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [match, setMatch] = useState('');
  const [summary, setSummary] = useState('');

  // Validation errors
  const [nameError, setNameError] = useState('');
  const [matchError, setMatchError] = useState('');
  const [summaryError, setSummaryError] = useState('');

  // Preview state
  const [previewResult, setPreviewResult] = useState<PolicyPreviewStatus | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState<Error | null>(null);

  // Sample data for autocomplete (fetched once when dialog opens)
  const [sampleData, setSampleData] = useState<unknown[]>([]);
  const [sampleDataLoading, setSampleDataLoading] = useState(false);

  // Extract field paths from sample data OR preview data for autocomplete
  const availableFields = useMemo(() => {
    // First try to use preview data if available
    if (previewResult?.fetchedInputs && previewResult.fetchedInputs.length > 0) {
      const dataObjects = previewResult.fetchedInputs.map(input => {
        if (input.type === 'audit' && input.audit) {
          return input.audit;
        } else if (input.type === 'event' && input.event) {
          return input.event;
        }
        return null;
      }).filter(Boolean);

      return extractFieldPathsFromMany(dataObjects);
    }

    // Fall back to sample data
    if (sampleData.length > 0) {
      return extractFieldPathsFromMany(sampleData);
    }

    return [];
  }, [previewResult?.fetchedInputs, sampleData]);

  // Initialize form when rule changes
  useEffect(() => {
    if (rule) {
      setName(rule.name || '');
      setDescription(rule.description || '');
      setMatch(rule.match || '');
      setSummary(rule.summary || '');
    } else {
      setName('');
      setDescription('');
      setMatch('');
      setSummary('');
    }
    // Clear errors when dialog opens/closes or rule changes
    setNameError('');
    setMatchError('');
    setSummaryError('');
    setPreviewResult(null);
    setPreviewError(null);
  }, [rule, open]);

  // Fetch sample data for autocomplete when dialog opens
  useEffect(() => {
    if (!open) {
      setSampleData([]);
      return;
    }

    let cancelled = false;
    setSampleDataLoading(true);

    async function fetchSampleData() {
      try {
        // Fetch sample data using a permissive match expression
        const preview = await apiClient.createPolicyPreview({
          policy: {
            resource: policyResource,
            auditRules: ruleType === 'audit' ? [{ name: 'sample', match: 'true', summary: 'sample' }] : undefined,
            eventRules: ruleType === 'event' ? [{ name: 'sample', match: 'true', summary: 'sample' }] : undefined,
          },
          autoFetch: {
            limit: 10,
            timeRange: '7d',
            sources: ruleType === 'audit' ? 'audit' : 'events',
          },
        });

        if (cancelled) return;

        // Extract data objects from fetched inputs
        const dataObjects = (preview.status?.fetchedInputs || []).map(input => {
          if (input.type === 'audit' && input.audit) return input.audit;
          if (input.type === 'event' && input.event) return input.event;
          return null;
        }).filter(Boolean);

        setSampleData(dataObjects);
      } catch (err) {
        // Silently fail - autocomplete just won't be available
        console.warn('Failed to fetch sample data for autocomplete:', err);
      } finally {
        if (!cancelled) {
          setSampleDataLoading(false);
        }
      }
    }

    fetchSampleData();

    return () => {
      cancelled = true;
    };
  }, [open, ruleType, policyResource, apiClient]);

  // Live preview - debounced
  useEffect(() => {
    // Don't run preview if dialog is closed or fields are empty
    if (!open || !match.trim() || !summary.trim()) {
      setPreviewResult(null);
      return;
    }

    const timer = setTimeout(async () => {
      setPreviewLoading(true);
      setPreviewError(null);

      try {
        // Create a preview using auto-fetch to get sample data from ClickHouse
        const preview = await apiClient.createPolicyPreview({
          policy: {
            resource: policyResource,
            auditRules: ruleType === 'audit' ? [{ name: name || 'preview', match, summary, description }] : undefined,
            eventRules: ruleType === 'event' ? [{ name: name || 'preview', match, summary, description }] : undefined,
          },
          autoFetch: {
            limit: 10,
            timeRange: '7d',
            sources: ruleType === 'audit' ? 'audit' : 'events',
          },
        });

        setPreviewResult(preview.status || null);
      } catch (err) {
        setPreviewError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setPreviewLoading(false);
      }
    }, 500); // Debounce 500ms

    return () => clearTimeout(timer);
  }, [open, match, summary, name, description, ruleType, policyResource, apiClient]);

  const celHelp = ruleType === 'audit' ? AUDIT_CEL_HELP : EVENT_CEL_HELP;
  const summaryHelp = ruleType === 'audit' ? AUDIT_SUMMARY_HELP : EVENT_SUMMARY_HELP;

  // Validate form
  const validate = (): boolean => {
    let isValid = true;

    // Validate name
    if (!name.trim()) {
      setNameError('Name is required');
      isValid = false;
    } else if (!isValidDNSSubdomain(name)) {
      setNameError('Name must be lowercase alphanumeric and hyphens (e.g., "create-rule")');
      isValid = false;
    } else if (existingNames.includes(name) && (!rule || rule.name !== name)) {
      setNameError('A rule with this name already exists');
      isValid = false;
    } else {
      setNameError('');
    }

    // Validate match expression
    if (!match.trim()) {
      setMatchError('Match expression is required');
      isValid = false;
    } else {
      setMatchError('');
    }

    // Validate summary
    if (!summary.trim()) {
      setSummaryError('Summary template is required');
      isValid = false;
    } else {
      setSummaryError('');
    }

    return isValid;
  };

  // Handle save
  const handleSave = () => {
    if (!validate()) {
      return;
    }

    const savedRule: ActivityPolicyRule = {
      name: name.trim(),
      description: description.trim() || undefined,
      match: match.trim(),
      summary: summary.trim(),
    };

    onSave(savedRule);
    onOpenChange(false);
  };

  // Handle cancel
  const handleCancel = () => {
    onCancel();
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full sm:max-w-2xl overflow-y-auto">
        <SheetHeader className="px-6">
          <SheetTitle className="text-base">
            {isCreating ? 'Create' : 'Edit'} {ruleType === 'audit' ? 'Audit' : 'Event'} Rule
          </SheetTitle>
          <SheetDescription className="text-xs">
            Define a rule to match {ruleType === 'audit' ? 'audit events' : 'Kubernetes events'} and generate activity summaries.
          </SheetDescription>
        </SheetHeader>

        <div className="px-6 py-3 text-sm flex-1 overflow-y-auto">
          {/* Form fields - full width */}
          <div className="space-y-3">
            {/* Name */}
            <div className="space-y-1">
              <Label htmlFor="rule-name" className="text-xs font-medium">
                Name <span className="text-destructive">*</span>
              </Label>
              <Input
                id="rule-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., create-rule"
                className={`h-8 text-sm ${nameError ? 'border-destructive' : ''}`}
              />
              {nameError && (
                <p className="text-[11px] text-destructive">{nameError}</p>
              )}
              <p className="text-[11px] text-muted-foreground">
                Lowercase alphanumeric and hyphens only
              </p>
            </div>

            {/* Description */}
            <div className="space-y-1">
              <Label htmlFor="rule-description" className="text-xs font-medium">Description</Label>
              <Textarea
                id="rule-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Optional description"
                rows={1}
                className="resize-none text-sm"
              />
            </div>

            {/* Match Expression */}
            <div className="space-y-1">
              <Label htmlFor="rule-match" className="text-xs font-medium">
                Match Expression (CEL) <span className="text-destructive">*</span>
              </Label>
              <CelEditor
                value={match}
                onChange={setMatch}
                language="cel"
                availableFields={availableFields}
                placeholder={celHelp.examples[0]}
                height={80}
                error={!!matchError}
                data-testid="cel-editor-match"
              />
              {matchError && (
                <p className="text-[11px] text-destructive">{matchError}</p>
              )}
              <details className="text-[11px]">
                <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                  Available variables
                </summary>
                <ul className="mt-1 space-y-0.5 pl-3 list-disc text-muted-foreground">
                  {celHelp.variables.map((v, i) => (
                    <li key={i}>{v}</li>
                  ))}
                </ul>
              </details>
            </div>

            {/* Summary Template */}
            <div className="space-y-1">
              <Label htmlFor="rule-summary" className="text-xs font-medium">
                Summary Template <span className="text-destructive">*</span>
              </Label>
              <CelEditor
                value={summary}
                onChange={setSummary}
                language="cel-template"
                availableFields={availableFields}
                placeholder={summaryHelp.examples[0]}
                height={80}
                error={!!summaryError}
                data-testid="cel-editor-summary"
              />
              {summaryError && (
                <p className="text-[11px] text-destructive">{summaryError}</p>
              )}
              <details className="text-[11px]">
                <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                  Template variables
                </summary>
                <ul className="mt-1 space-y-0.5 pl-3 list-disc text-muted-foreground">
                  {summaryHelp.variables.map((v, i) => (
                    <li key={i}>{v}</li>
                  ))}
                </ul>
              </details>
            </div>
          </div>

          {/* Live preview - below the form */}
          <div className="space-y-1 mt-4 pt-4 border-t">
            <h3 className="text-xs font-medium">Live Preview</h3>
            <p className="text-[11px] text-muted-foreground">
              Preview updates as you type.
            </p>
            <PolicyPreviewPanel
              result={previewResult}
              inputs={[]}
              isLoading={previewLoading}
              error={previewError}
            />
          </div>
        </div>

        <SheetFooter className="mt-4 pt-4 border-t px-6">
          <Button type="tertiary" theme="outline" size="small" onClick={handleCancel}>
            Cancel
          </Button>
          <Button size="small" onClick={handleSave}>
            {isCreating ? 'Create Rule' : 'Save Changes'}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
