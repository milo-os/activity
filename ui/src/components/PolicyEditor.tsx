import { useEffect, useCallback, useState } from 'react';
import type { Condition } from '../types/policy';
import type { ResourceRef, ErrorFormatter } from '../types/activity';
import { ActivityApiClient } from '../api/client';
import { usePolicyEditor, type UsePolicyEditorResult } from '../hooks/usePolicyEditor';
import { PolicyResourceForm } from './PolicyResourceForm';
import { PolicyRuleList } from './PolicyRuleList';
import { PolicyActivityView } from './PolicyActivityView';
import { Input } from '@datum-cloud/datum-ui/input';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardHeader, CardContent } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';
import { Label } from '@datum-cloud/datum-ui/label';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@datum-cloud/datum-ui/tabs';
import { ApiErrorAlert } from './ApiErrorAlert';
import { Alert, AlertDescription } from '@datum-cloud/datum-ui/alert';
import { AlertTriangle, AlertCircle, Trash2, Copy, Check } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from './ui/tooltip';

export interface PolicyEditorProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Policy name to edit (undefined for new policy) */
  policyName?: string;
  /** Callback when save succeeds */
  onSaveSuccess?: (policyName: string) => void;
  /** Callback when cancel is clicked */
  onCancel?: () => void;
  /** Handler for resource link clicks in preview */
  onResourceClick?: (resource: ResourceRef) => void;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

/**
 * PolicyEditor provides a full editor for creating/editing ActivityPolicies
 * with a preview panel for testing
 */
export function PolicyEditor({
  client,
  policyName,
  onSaveSuccess,
  onCancel,
  onResourceClick,
  className = '',
  errorFormatter,
}: PolicyEditorProps) {
  // Editor state
  const editor: UsePolicyEditorResult = usePolicyEditor({
    client,
    initialPolicyName: policyName,
  });

  // Delete confirmation state
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // Copy state
  const [isCopied, setIsCopied] = useState(false);

  // Track active tab
  const [activeTab, setActiveTab] = useState<string>('activity');

  // Load existing policy on mount
  useEffect(() => {
    if (policyName) {
      editor.load(policyName).catch((err) => {
        console.error('Failed to load policy:', err);
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [policyName]);

  // Handle save
  const handleSave = useCallback(
    async (dryRun: boolean = false) => {
      try {
        const result = await editor.save(dryRun);
        if (!dryRun && onSaveSuccess && result.metadata?.name) {
          onSaveSuccess(result.metadata.name);
        }
      } catch (err) {
        // Error is stored in editor.error
        console.error('Save failed:', err);
      }
    },
    [editor, onSaveSuccess]
  );

  // Handle delete
  const handleDelete = useCallback(async () => {
    if (!policyName) return;

    setIsDeleting(true);
    try {
      await client.deletePolicy(policyName);
      setShowDeleteDialog(false);
      if (onSaveSuccess) {
        // Navigate away after successful delete
        onSaveSuccess('');
      }
    } catch (err) {
      console.error('Failed to delete policy:', err);
    } finally {
      setIsDeleting(false);
    }
  }, [client, policyName, onSaveSuccess]);

  // Handle copy resource name
  const handleCopyResourceName = useCallback(async () => {
    if (editor.name) {
      try {
        await navigator.clipboard.writeText(editor.name);
        setIsCopied(true);
        setTimeout(() => setIsCopied(false), 2000);
      } catch (err) {
        console.error('Failed to copy resource name:', err);
      }
    }
  }, [editor.name]);

  // Validation
  const canSave =
    editor.name.trim() !== '' &&
    editor.spec.resource.apiGroup.trim() !== '' &&
    editor.spec.resource.kind.trim() !== '';

  // Get policy status from conditions
  const getPolicyStatus = (): {
    status: 'ready' | 'error' | 'pending' | 'unknown';
    message?: string;
  } | null => {
    if (!editor.policy || editor.isNew) {
      return null; // No status for new policies
    }

    const conditions = editor.policy.status?.conditions;
    if (!conditions || conditions.length === 0) {
      // Default to ready if no conditions exist (policy is healthy by default)
      return { status: 'ready', message: 'Policy is active' };
    }

    const readyCondition = conditions.find((c: Condition) => c.type === 'Ready');
    if (!readyCondition) {
      // No Ready condition means policy is healthy (default state)
      return { status: 'ready', message: 'Policy is active' };
    }

    if (readyCondition.status === 'True') {
      return { status: 'ready', message: readyCondition.message || 'All rules compiled successfully' };
    } else if (readyCondition.status === 'False') {
      return { status: 'error', message: readyCondition.message || readyCondition.reason || 'Rule compilation failed' };
    }

    return { status: 'pending', message: readyCondition.message || 'Status unknown' };
  };

  const policyStatus = getPolicyStatus();

  return (
    <TooltipProvider delayDuration={0}>
      <Card className={`rounded-xl ${className}`}>
        {/* Header */}
        <CardHeader className="flex flex-row justify-between items-center p-4 border-b border-border space-y-0">
          <div className="flex items-center gap-3">
            {editor.isNew ? (
              <div className="flex flex-col gap-0.5">
                <Label htmlFor="policy-name" className="text-xs text-muted-foreground">
                  Policy Name
                </Label>
                <Input
                  id="policy-name"
                  type="text"
                  className="w-[300px] text-base font-medium"
                  value={editor.name}
                  onChange={(e) => editor.setName(e.target.value)}
                  placeholder="e.g., httpproxy-policy"
                />
              </div>
            ) : (
              <div className="flex flex-col gap-0.5">
                <div className="flex items-center gap-1.5">
                  {policyStatus && (
                    <Tooltip delayDuration={300}>
                      <TooltipTrigger asChild>
                        <span
                          className={`w-2 h-2 rounded-full ${
                            policyStatus.status === 'ready'
                              ? 'bg-green-500'
                              : policyStatus.status === 'error'
                              ? 'bg-red-500'
                              : 'bg-yellow-500'
                          }`}
                          aria-label={`Policy status: ${policyStatus.status}`}
                        />
                      </TooltipTrigger>
                      <TooltipContent>
                        <p className="text-xs">{policyStatus.message}</p>
                      </TooltipContent>
                    </Tooltip>
                  )}
                  <h2 className="m-0 text-xl font-semibold text-foreground leading-tight">
                    {editor.spec.resource.kind || 'Untitled Policy'}
                  </h2>
                </div>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  {editor.spec.resource.apiGroup && (
                    <>
                      <span>API Group: {editor.spec.resource.apiGroup}</span>
                      <span className="text-muted-foreground/50">•</span>
                    </>
                  )}
                  <span>Resource: {editor.name}</span>
                  <Tooltip delayDuration={500}>
                    <TooltipTrigger asChild>
                      <button
                        onClick={handleCopyResourceName}
                        className="inline-flex items-center justify-center p-0.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-opacity cursor-pointer"
                        aria-label="Copy resource name"
                      >
                        {isCopied ? (
                          <Check className="h-3 w-3 text-green-600 dark:text-green-400" />
                        ) : (
                          <Copy className="h-3 w-3 text-gray-500 dark:text-gray-400" />
                        )}
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p className="text-xs">Click to copy</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
            )}
          </div>

        <div className="flex gap-2">
          {onCancel && (
            <Button
              htmlType="button"
              type="tertiary" theme="outline"
              size="small"
              onClick={onCancel}
              disabled={editor.isSaving}
              className="h-7 text-xs"
            >
              Cancel
            </Button>
          )}
          <Button
            htmlType="button"
            type="tertiary" theme="outline"
            size="small"
            onClick={() => handleSave(true)}
            disabled={!canSave || editor.isSaving}
            title="Validate without saving"
            className="h-7 text-xs"
          >
            Validate
          </Button>
          <Button
            htmlType="button"
            size="small"
            onClick={() => handleSave(false)}
            disabled={!canSave || editor.isSaving || !editor.isDirty}
            className="bg-primary text-primary-foreground border-primary hover:bg-primary/90 hover:border-primary/90 h-7 text-xs"
          >
            {editor.isSaving ? (
              <>
                <span className="w-3.5 h-3.5 border-2 border-border border-t-[#BF9595] rounded-full animate-spin" />
                Saving...
              </>
            ) : (
              'Save Policy'
            )}
          </Button>
        </div>
      </CardHeader>

      {/* Error Display */}
      <ApiErrorAlert error={editor.error} className="mx-4 mt-3" errorFormatter={errorFormatter} />

      {/* Loading State */}
      {editor.isLoading && (
        <div className="flex items-center justify-center gap-2 py-8 text-muted-foreground">
          <span className="w-5 h-5 border-[3px] border-border border-t-[#BF9595] rounded-full animate-spin" />
          Loading policy...
        </div>
      )}

      {/* Main Content */}
      {!editor.isLoading && (
        <CardContent className="p-4">
          <Tabs defaultValue="activity" value={activeTab} onValueChange={setActiveTab} className="w-full">
            <TabsList className="grid w-full grid-cols-2 mb-4">
              <TabsTrigger value="activity">Activity</TabsTrigger>
              <TabsTrigger value="editor">Editor</TabsTrigger>
            </TabsList>

            {/* Activity Tab */}
            <TabsContent value="activity" className="mt-0">
              <PolicyActivityView
                client={client}
                policyResource={editor.spec.resource}
                policyName={editor.name}
                onResourceClick={onResourceClick}
                errorFormatter={errorFormatter}
              />
            </TabsContent>

            {/* Editor Tab */}
            <TabsContent value="editor" className="mt-0">
              <div className="flex flex-col gap-4">
                <PolicyResourceForm
                  resource={editor.spec.resource}
                  onChange={editor.setResource}
                  client={client}
                  isEditMode={!editor.isNew}
                />

                <PolicyRuleList
                  auditRules={editor.spec.auditRules || []}
                  eventRules={editor.spec.eventRules || []}
                  policyResource={editor.spec.resource}
                  apiClient={client}
                  onAuditRulesChange={editor.setAuditRules}
                  onEventRulesChange={editor.setEventRules}
                />

                {/* Danger Zone - Delete Policy (only for existing policies) */}
                {!editor.isNew && (
                  <div className="mt-6 pt-4 border-t border-border">
                    <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4">
                      <div className="flex items-start gap-3">
                        <div className="flex-1">
                          <h3 className="text-base font-semibold text-foreground mb-1.5 flex items-center gap-2">
                            <AlertTriangle className="h-5 w-5 text-destructive" />
                            Danger Zone
                          </h3>
                          <p className="text-sm text-muted-foreground mb-3">
                            Deleting this policy will stop translating audit logs and events for{' '}
                            <strong className="text-foreground">
                              {editor.spec.resource.kind}
                            </strong>{' '}
                            resources. Existing activities will be preserved, but no new activities will be generated.
                          </p>
                          <Button
                            type="danger" theme="solid"
                            size="small"
                            onClick={() => setShowDeleteDialog(true)}
                            className="h-8 text-xs"
                          >
                            <Trash2 className="h-3.5 w-3.5 mr-1.5" />
                            Delete Policy
                          </Button>
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </TabsContent>
          </Tabs>
        </CardContent>
      )}

      {/* Delete Confirmation Dialog */}
      <Dialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <DialogContent className="max-w-[500px]">
          <DialogHeader>
            <DialogTitle>Delete Policy</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete{' '}
              <strong className="text-foreground">{editor.name}</strong>?
            </DialogDescription>
          </DialogHeader>
          <Alert variant="destructive" className="mt-2">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>
              <strong>This action cannot be undone.</strong>
              <br />
              Activities already generated by this policy will remain in the system,
              but no new activities will be created for {editor.spec.resource.kind} resources.
            </AlertDescription>
          </Alert>
          <DialogFooter>
            <Button
              type="tertiary" theme="outline"
              size="small"
              onClick={() => setShowDeleteDialog(false)}
              disabled={isDeleting}
              className="h-8 text-xs"
            >
              Cancel
            </Button>
            <Button
              type="danger" theme="solid"
              size="small"
              onClick={handleDelete}
              disabled={isDeleting}
              className="h-8 text-xs"
            >
              {isDeleting ? (
                <>
                  <span className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin mr-2" />
                  Deleting...
                </>
              ) : (
                <>
                  <Trash2 className="h-3.5 w-3.5 mr-1.5" />
                  Delete Policy
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
    </TooltipProvider>
  );
}
