// Components - Audit Log Query (existing)
export { FilterBuilder } from './components/FilterBuilder';
export { FilterBuilderWithAutocomplete } from './components/FilterBuilderWithAutocomplete';
export { SimpleQueryBuilder } from './components/SimpleQueryBuilder';
export { AuditEventViewer } from './components/AuditEventViewer';
export { AuditLogQueryComponent } from './components/AuditLogQueryComponent';
export { AuditLogFeedItem } from './components/AuditLogFeedItem';
export type { AuditLogFeedItemProps } from './components/AuditLogFeedItem';
export { AuditLogExpandedDetails } from './components/AuditLogExpandedDetails';
export type { AuditLogExpandedDetailsProps } from './components/AuditLogExpandedDetails';
export { DateTimeRangePicker } from './components/DateTimeRangePicker';
export type { DateTimeRange, DateTimeRangePickerProps } from './components/DateTimeRangePicker';
export { AuditLogFilters, buildAuditLogCEL } from './components/AuditLogFilters';
export type { AuditLogFiltersProps, AuditLogFilterState } from './components/AuditLogFilters';
export { ActionToggle } from './components/ActionToggle';
export type { ActionToggleProps, ActionOption } from './components/ActionToggle';
export { ActionMultiSelect } from './components/ActionMultiSelect';
export type { ActionMultiSelectProps, ActionMultiSelectOption } from './components/ActionMultiSelect';
export { UserSelect } from './components/UserSelect';
export type { UserSelectProps, UserOption } from './components/UserSelect';

// Components - Activity Feed (new)
export { ActivityFeed } from './components/ActivityFeed';
export type { ActivityFeedProps } from './components/ActivityFeed';
export { ActivityFeedItem } from './components/ActivityFeedItem';
export type { ActivityFeedItemProps } from './components/ActivityFeedItem';
export { ActivityFeedItemSkeleton } from './components/ActivityFeedItemSkeleton';
export type { ActivityFeedItemSkeletonProps } from './components/ActivityFeedItemSkeleton';
export { ActivityFeedSummary } from './components/ActivityFeedSummary';
export type { ActivityFeedSummaryProps, ResourceLinkClickHandler } from './components/ActivityFeedSummary';
export type { ActivityFeedFiltersProps } from './components/ActivityFeedFilters';
export { ChangeSourceToggle } from './components/ChangeSourceToggle';
export type { ChangeSourceToggleProps, ChangeSourceOption } from './components/ChangeSourceToggle';
export { ResourceHistoryView } from './components/ResourceHistoryView';
export type { ResourceHistoryViewProps, ResourceFilter } from './components/ResourceHistoryView';
export { ActivityExpandedDetails } from './components/ActivityExpandedDetails';
export type { ActivityExpandedDetailsProps } from './components/ActivityExpandedDetails';
export { TenantBadge } from './components/TenantBadge';
export type { TenantBadgeProps } from './components/TenantBadge';

// Components - Events Feed (new)
export { EventsFeed } from './components/EventsFeed';
export type { EventsFeedProps } from './components/EventsFeed';
export { EventFeedItem } from './components/EventFeedItem';
export type { EventFeedItemProps } from './components/EventFeedItem';
export type { EventsFeedFiltersProps } from './components/EventsFeedFilters';
export { EventTypeToggle } from './components/EventTypeToggle';
export type { EventTypeToggleProps, EventTypeOption } from './components/EventTypeToggle';
export { EventExpandedDetails } from './components/EventExpandedDetails';
export type { EventExpandedDetailsProps } from './components/EventExpandedDetails';

// Components - Policy Authoring (new)
export { PolicyList } from './components/PolicyList';
export type { PolicyListProps } from './components/PolicyList';
export { PolicyEditor } from './components/PolicyEditor';
export type { PolicyEditorProps } from './components/PolicyEditor';
export { PolicyDetailView } from './components/PolicyDetailView';
export type { PolicyDetailViewProps } from './components/PolicyDetailView';
export { PolicyEditView } from './components/PolicyEditView';
export type { PolicyEditViewProps } from './components/PolicyEditView';
export { PolicyActivityView } from './components/PolicyActivityView';
export type { PolicyActivityViewProps } from './components/PolicyActivityView';
export { PolicyActivityViewSkeleton } from './components/PolicyActivityViewSkeleton';
export type { PolicyActivityViewSkeletonProps } from './components/PolicyActivityViewSkeleton';
export { PolicyPreviewPanel } from './components/PolicyPreviewPanel';
export type { PolicyPreviewPanelProps } from './components/PolicyPreviewPanel';
export { PolicyPreviewResult } from './components/PolicyPreviewResult';
export type { PolicyPreviewResultProps } from './components/PolicyPreviewResult';
export { PolicyRuleList } from './components/PolicyRuleList';
export type { PolicyRuleListProps } from './components/PolicyRuleList';
export { PolicyRuleEditor } from './components/PolicyRuleEditor';
export type { PolicyRuleEditorProps } from './components/PolicyRuleEditor';
export { PolicyRuleEditorDialog } from './components/PolicyRuleEditorDialog';
export type { PolicyRuleEditorDialogProps } from './components/PolicyRuleEditorDialog';
export { PolicyRuleListItem } from './components/PolicyRuleListItem';
export type { PolicyRuleListItemProps } from './components/PolicyRuleListItem';
export { PolicyResourceForm } from './components/PolicyResourceForm';
export type { PolicyResourceFormProps } from './components/PolicyResourceForm';
export { SampleInputTemplates, AUDIT_TEMPLATES, EVENT_TEMPLATES } from './components/SampleInputTemplates';
export type { SampleInputTemplatesProps } from './components/SampleInputTemplates';
export { RulePreviewPanel } from './components/RulePreviewPanel';
export type { RulePreviewPanelProps } from './components/RulePreviewPanel';
export { CelEditor } from './components/CelEditor';
export type { CelEditorProps } from './components/CelEditor';

// Components - Activity Layout
export { ActivityLayout } from './components/ActivityLayout';
export type { ActivityLayoutProps, ActivityTab } from './components/ActivityLayout';

// Components - ReindexJob
export { ReindexJobList } from './components/ReindexJobList';
export type { ReindexJobListProps } from './components/ReindexJobList';
export { ReindexJobDetailView } from './components/ReindexJobDetailView';
export type { ReindexJobDetailViewProps } from './components/ReindexJobDetailView';
export { ReindexJobCreate } from './components/ReindexJobCreate';
export type { ReindexJobCreateProps } from './components/ReindexJobCreate';
export { ReindexJobDialog } from './components/ReindexJobDialog';
export type { ReindexJobDialogProps } from './components/ReindexJobDialog';

// UI Components (shadcn/ui based)
export {
  Select,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectLabel,
  SelectItem,
  SelectSeparator,
} from '@datum-cloud/datum-ui/select';

export { Button, buttonVariants } from '@datum-cloud/datum-ui/button';
export type { ButtonProps } from '@datum-cloud/datum-ui/button';

export {
  Card,
  CardHeader,
  CardFooter,
  CardTitle,
  CardDescription,
  CardContent,
} from '@datum-cloud/datum-ui/card';

export { Badge, badgeVariants } from './components/ui/badge';
export type { BadgeProps } from './components/ui/badge';

export { Alert, AlertTitle, AlertDescription } from '@datum-cloud/datum-ui/alert';

export { ApiErrorAlert } from './components/ApiErrorAlert';
export type { ApiErrorAlertProps } from './components/ApiErrorAlert';

export { Checkbox } from '@datum-cloud/datum-ui/checkbox';

// datum-ui's primitives don't expose explicit *Props type aliases, so we
// derive them from each component's prop signature for source-compat.
import type { ComponentProps } from 'react';
import { Input as DUIInput } from '@datum-cloud/datum-ui/input';
import { Label as DUILabel } from '@datum-cloud/datum-ui/label';
import { Textarea as DUITextarea } from '@datum-cloud/datum-ui/textarea';

export { Textarea } from '@datum-cloud/datum-ui/textarea';
export type TextareaProps = ComponentProps<typeof DUITextarea>;

export { Input } from '@datum-cloud/datum-ui/input';
export type InputProps = ComponentProps<typeof DUIInput>;

export { Label } from '@datum-cloud/datum-ui/label';
export type LabelProps = ComponentProps<typeof DUILabel>;

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogClose,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
} from './components/ui/dialog';

export { Tabs, TabsList, TabsTrigger, TabsContent } from '@datum-cloud/datum-ui/tabs';

export { Separator } from '@datum-cloud/datum-ui/separator';

export {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from './components/ui/tooltip';

export { Skeleton } from '@datum-cloud/datum-ui/skeleton';

export { Combobox } from './components/ui/combobox';
export type { ComboboxProps, ComboboxOption } from './components/ui/combobox';

export { MultiCombobox } from './components/ui/multi-combobox';
export type { MultiComboboxProps, MultiComboboxOption } from './components/ui/multi-combobox';

export { TimeRangeDropdown } from './components/ui/time-range-dropdown';
export type { TimeRangeDropdownProps, TimeRangePreset } from './components/ui/time-range-dropdown';

// Utilities
export { cn } from './lib/utils';
export { ApiError, parseApiError, NetworkError, defaultErrorFormatter } from './lib/errors';
export type { ApiErrorResponse } from './lib/errors';
export { extractFieldPaths, extractFieldPathsFromMany } from './lib/extractFieldPaths';

// Utilities - Deep Linking & URL Serialization
export {
  serializeActivityFilters,
  parseActivityFilters,
  serializeEventFilters,
  parseEventFilters,
  parseTimeRange,
} from './lib/activity-filters';

// Utilities - Shareable URLs
export { generateShareableUrl, copyToClipboard } from './lib/activity-share';

// Hooks - Audit Log Query (existing)
export { useAuditLogQuery } from './hooks/useAuditLogQuery';
export { useAuditLogFacets } from './hooks/useAuditLogFacets';
export type {
  UseAuditLogFacetsResult,
  AuditLogTimeRange,
} from './hooks/useAuditLogFacets';

// Hooks - Activity Feed (new)
export { useActivityFeed } from './hooks/useActivityFeed';
export type {
  UseActivityFeedOptions,
  UseActivityFeedResult,
  ActivityFeedFilters,
  ActivityFeedFilters as ActivityFeedFilterState,
  TimeRange,
} from './hooks/useActivityFeed';
export { useFacets } from './hooks/useFacets';
export type { UseFacetsResult } from './hooks/useFacets';

// Hooks - Events Feed (new)
export { useEventsFeed } from './hooks/useEventsFeed';
export type {
  UseEventsFeedOptions,
  UseEventsFeedResult,
  EventsFeedFilters,
  EventsFeedFilters as EventsFeedFilterState,
  TimeRange as EventsTimeRange,
} from './hooks/useEventsFeed';
export { useEventFacets } from './hooks/useEventFacets';
export type { UseEventFacetsResult } from './hooks/useEventFacets';

// Hooks - Policy Authoring (new)
export { usePolicyList } from './hooks/usePolicyList';
export type {
  UsePolicyListOptions,
  UsePolicyListResult,
} from './hooks/usePolicyList';
export { usePolicyEditor } from './hooks/usePolicyEditor';
export type {
  UsePolicyEditorOptions,
  UsePolicyEditorResult,
} from './hooks/usePolicyEditor';
export { usePolicyPreview } from './hooks/usePolicyPreview';
export type {
  UsePolicyPreviewOptions,
  UsePolicyPreviewResult,
} from './hooks/usePolicyPreview';

// Hooks - ReindexJob
export { useReindexJobs } from './hooks/useReindexJobs';
export type {
  UseReindexJobsOptions,
  UseReindexJobsResult,
} from './hooks/useReindexJobs';

// API Client
export { ActivityApiClient } from './api/client';
export type { ApiClientConfig } from './api/client';

// Types - Audit Log (existing)
export type {
  Event,
  ObjectReference,
  UserInfo,
  QueryPhase,
  AuditLogQuerySpec,
  AuditLogQueryStatus,
  AuditLogQuery,
  AuditLog,
  FilterField,
} from './types';

export { FILTER_FIELDS } from './types';

// Types - Activity (new)
export type {
  Activity,
  ActivitySpec,
  ActivityList,
  ActivityListParams,
  ActivityLink,
  ResourceRef,
  Actor,
  ActorType,
  ChangeSource,
  OriginType,
  TenantType,
  Tenant,
  FieldChange,
  ActivityOrigin,
  ActivityFacetQuery,
  ActivityFacetQuerySpec,
  ActivityFacetQueryStatus,
  FacetSpec,
  FacetResult,
  FacetValue,
  ActivityFilterField,
  WatchEvent,
  WatchEventType,
  WatchErrorStatus,
  ResourceLinkResolver,
  TenantLinkResolver,
  TenantRenderer,
  FormattedError,
  ErrorFormatter,
  EffectiveTimeRange,
  EffectiveTimeRangeCallback,
} from './types/activity';

export { ACTIVITY_FILTER_FIELDS, defaultResourceLinkResolver } from './types/activity';

// Types - Kubernetes Events (new)
export type {
  K8sEvent,
  K8sEventList,
  K8sEventListParams,
  K8sEventType,
  ObjectReference as K8sObjectReference,
  EventSeries,
  EventRecord,
  EventQuery,
  EventQuerySpec,
  EventQueryStatus,
  EventFacetQuery,
  EventFacetQuerySpec,
  EventFacetQueryStatus,
  EventFilterField,
  EventFacetField,
} from './types/k8s-event';

export { EVENT_FILTER_FIELDS, EVENT_FACET_FIELDS, extractEvent, isEventRecord } from './types/k8s-event';

// Types - Policy Authoring (new)
export type {
  ActivityPolicy,
  ActivityPolicySpec,
  ActivityPolicyStatus,
  ActivityPolicyRule,
  ActivityPolicyResource,
  ActivityPolicyList,
  Condition,
  PolicyPreview,
  PolicyPreviewSpec,
  PolicyPreviewInput,
  PolicyPreviewInputType,
  PolicyPreviewStatus,
  PolicyPreviewPolicySpec,
  AutoFetchSpec,
  KubernetesEvent,
  PolicyGroup,
  SampleInputTemplate,
  PolicyFilterField,
} from './types/policy';

export { POLICY_FILTER_FIELDS } from './types/policy';

// Types - ReindexJob
export type {
  ReindexJob,
  ReindexJobSpec,
  ReindexJobStatus,
  ReindexJobPhase,
  ReindexJobListResource,
  ReindexTimeRange,
  ReindexPolicySelector,
  CreateReindexJobParams,
} from './types/reindex';

export {
  isReindexJobTerminal,
  isReindexJobRunning,
  getReindexJobStatusMessage,
  getReindexJobDuration,
} from './types/reindex';
