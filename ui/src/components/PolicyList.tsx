import { useEffect, useState, useCallback } from "react";
import type { ActivityPolicy, Condition } from "../types/policy";
import type { ErrorFormatter } from "../types/activity";
import { ActivityApiClient } from "../api/client";
import {
  usePolicyList,
  type UsePolicyListResult,
} from "../hooks/usePolicyList";
import { Button } from "@datum-cloud/datum-ui/button";
import { Card, CardContent, CardHeader } from "@datum-cloud/datum-ui/card";
import { Badge } from "./ui/badge";
import { Separator } from "@datum-cloud/datum-ui/separator";
import { Skeleton } from "@datum-cloud/datum-ui/skeleton";
import { ApiErrorAlert } from "./ApiErrorAlert";
import { AlertTriangle } from "lucide-react";

export interface PolicyListProps {
  /** API client instance */
  client: ActivityApiClient;
  /** Callback when a policy is selected for viewing */
  onViewPolicy?: (policyName: string) => void;
  /** Callback when a policy is selected for editing (deprecated - use onViewPolicy) */
  onEditPolicy?: (policyName: string) => void;
  /** Callback when create new policy is requested */
  onCreatePolicy?: () => void;
  /** Whether to show grouped by API group (default: true) */
  groupByApiGroup?: boolean;
  /** Additional CSS class */
  className?: string;
  /** Custom error formatter for customizing error messages */
  errorFormatter?: ErrorFormatter;
}

/**
 * Skeleton row for table loading state
 */
function PolicySkeletonRow() {
  return (
    <tr>
      <td className="px-3 py-2 border-b border-border">
        <div className="flex items-center gap-2">
          <Skeleton className="w-2 h-2 rounded-full" />
          <Skeleton className="h-3 w-24" />
        </div>
      </td>
      <td className="px-3 py-2 border-b border-border text-center w-24">
        <Skeleton className="h-5 w-8 mx-auto rounded-full" />
      </td>
      <td className="px-3 py-2 border-b border-border text-center w-24">
        <Skeleton className="h-5 w-8 mx-auto rounded-full" />
      </td>
    </tr>
  );
}

/**
 * Skeleton group for loading state
 */
function PolicySkeletonGroup() {
  return (
    <div className="border border-input rounded-lg overflow-hidden">
      <div className="w-full flex items-center gap-2.5 px-3 py-2 bg-muted">
        <Skeleton className="h-3 w-3" />
        <Skeleton className="h-3 flex-1 max-w-[200px]" />
        <Skeleton className="h-5 w-8 rounded-full" />
      </div>
      <div className="p-1.5">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr>
              <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap">
                Kind
              </th>
              <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input w-24 text-xs whitespace-nowrap">
                Audit Rules
              </th>
              <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input w-24 text-xs whitespace-nowrap">
                Event Rules
              </th>
              <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input w-16 text-xs whitespace-nowrap"></th>
            </tr>
          </thead>
          <tbody>
            <PolicySkeletonRow />
            <PolicySkeletonRow />
            <PolicySkeletonRow />
          </tbody>
        </table>
      </div>
    </div>
  );
}

/**
 * PolicyList displays all ActivityPolicies with edit actions
 */
export function PolicyList({
  client,
  onViewPolicy,
  onEditPolicy,
  onCreatePolicy,
  groupByApiGroup = true,
  className = "",
  errorFormatter,
}: PolicyListProps) {
  const policyList: UsePolicyListResult = usePolicyList({
    client,
    groupByApiGroup,
  });

  // Use onViewPolicy if provided, otherwise fall back to onEditPolicy for backwards compatibility
  const handlePolicyClick = onViewPolicy || onEditPolicy;

  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  // Load policies on mount
  useEffect(() => {
    policyList.refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Expand all groups by default when data loads
  useEffect(() => {
    if (policyList.groups.length > 0 && expandedGroups.size === 0) {
      setExpandedGroups(new Set(policyList.groups.map((g) => g.apiGroup)));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [policyList.groups]);

  // Toggle group expansion
  const toggleGroup = useCallback((apiGroup: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(apiGroup)) {
        next.delete(apiGroup);
      } else {
        next.add(apiGroup);
      }
      return next;
    });
  }, []);

  // Count rules for display
  const countRules = (
    policy: ActivityPolicy,
  ): { audit: number; event: number } => {
    return {
      audit: policy.spec.auditRules?.length || 0,
      event: policy.spec.eventRules?.length || 0,
    };
  };

  // Get policy status from conditions
  const getPolicyStatus = (
    policy: ActivityPolicy,
  ): {
    status: "ready" | "error" | "pending" | "unknown";
    message?: string;
  } => {
    const conditions = policy.status?.conditions;
    if (!conditions || conditions.length === 0) {
      return { status: "unknown", message: "Status not yet available" };
    }

    const readyCondition = conditions.find(
      (c: Condition) => c.type === "Ready",
    );
    if (!readyCondition) {
      return { status: "unknown", message: "Status not yet available" };
    }

    if (readyCondition.status === "True") {
      return {
        status: "ready",
        message: readyCondition.message || "All rules compiled successfully",
      };
    } else if (readyCondition.status === "False") {
      return {
        status: "error",
        message:
          readyCondition.message ||
          readyCondition.reason ||
          "Rule compilation failed",
      };
    }

    return {
      status: "pending",
      message: readyCondition.message || "Status unknown",
    };
  };

  return (
    <Card className={`rounded-xl ${className}`}>
      {/* Header */}
      <CardHeader className="pb-1.5">
        <div className="flex justify-between items-center">
          <p className="text-sm text-muted-foreground m-0">
            Turn cryptic audit events into activity timelines your team will
            actually enjoy reading
          </p>
          <div className="flex gap-1.5">
            <Button
              type="tertiary"
              theme="outline"
              size="small"
              onClick={policyList.refresh}
              disabled={policyList.isLoading}
              title="Refresh policy list"
              className="h-7 px-2"
            >
              {policyList.isLoading ? (
                <span className="w-3.5 h-3.5 border-2 border-border border-t-primary rounded-full animate-spin" />
              ) : (
                "↻"
              )}
            </Button>
            {onCreatePolicy && (
              <Button
                size="small"
                onClick={onCreatePolicy}
                className="bg-primary text-primary-foreground hover:bg-primary/90 h-7 text-xs"
              >
                Create Policy
              </Button>
            )}
          </div>
        </div>
        <Separator className="mt-1.5" />
      </CardHeader>

      <CardContent className="pt-3">
        {/* Error Display */}
        <ApiErrorAlert
          error={policyList.error}
          onRetry={policyList.refresh}
          className="mb-3"
          errorFormatter={errorFormatter}
        />

        {/* Loading State - Skeleton */}
        {policyList.isLoading && policyList.policies.length === 0 && (
          <div className="flex flex-col gap-2.5">
            <PolicySkeletonGroup />
            <PolicySkeletonGroup />
          </div>
        )}

        {/* Empty State */}
        {!policyList.isLoading &&
          !policyList.error &&
          policyList.policies.length === 0 && (
            <div className="text-center py-8 px-6 text-muted-foreground">
              <div className="text-4xl mb-3">📋</div>
              <h3 className="m-0 mb-1.5 text-foreground">No policies found</h3>
              <p className="m-0 mb-4 max-w-[400px] mx-auto">
                Activity policies define how audit events and Kubernetes events
                are translated into human-readable activity summaries.
              </p>
              {onCreatePolicy && (
                <Button
                  size="small"
                  onClick={onCreatePolicy}
                  className="bg-primary text-primary-foreground hover:bg-primary/90 h-7 text-xs"
                >
                  Create your first policy
                </Button>
              )}
            </div>
          )}

        {/* Policy Groups */}
        {policyList.groups.length > 0 && (
          <div className="flex flex-col gap-2.5">
            {policyList.groups.map((group) => (
              <div
                key={group.apiGroup}
                className="border border-input rounded-lg overflow-hidden"
              >
                <button
                  type="button"
                  className="w-full flex items-center gap-2.5 px-3 py-2 bg-muted border-none cursor-pointer text-left text-xs font-medium text-foreground transition-colors duration-200 hover:bg-accent"
                  onClick={() => toggleGroup(group.apiGroup)}
                >
                  <span
                    className={`text-xs text-muted-foreground transition-transform duration-200 ${
                      expandedGroups.has(group.apiGroup) ? "rotate-90" : ""
                    }`}
                  >
                    ▶
                  </span>
                  <span className="flex-1 font-mono">{group.apiGroup}</span>
                  <Badge variant="secondary" className="rounded-full">
                    {group.policies.length}
                  </Badge>
                </button>

                {expandedGroups.has(group.apiGroup) && (
                  <div className="p-1.5">
                    <table className="w-full border-collapse text-sm">
                      <thead>
                        <tr>
                          <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input text-xs whitespace-nowrap">
                            Kind
                          </th>
                          <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input w-24 text-xs whitespace-nowrap">
                            Audit Rules
                          </th>
                          <th className="text-left px-3 py-2 bg-muted text-muted-foreground font-medium border-b border-input w-24 text-xs whitespace-nowrap">
                            Event Rules
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {group.policies.map((policy) => {
                          const rules = countRules(policy);
                          const policyStatus = getPolicyStatus(policy);
                          return (
                            <tr
                              key={policy.metadata?.name}
                              className={`hover:bg-muted transition-colors ${handlePolicyClick ? "cursor-pointer" : ""}`}
                              onClick={() =>
                                handlePolicyClick?.(policy.metadata?.name || "")
                              }
                            >
                              <td className="px-3 py-2 border-b border-border last:border-b-0">
                                <div className="flex items-center gap-2">
                                  {policyStatus.status === "ready" ? (
                                    <div
                                      className="w-2 h-2 rounded-full bg-green-500"
                                      title={policyStatus.message}
                                    />
                                  ) : (
                                    <div title={policyStatus.message}>
                                      <AlertTriangle
                                        className={`w-4 h-4 ${
                                          policyStatus.status === "error"
                                            ? "text-red-500 dark:text-red-400"
                                            : policyStatus.status === "pending"
                                              ? "text-amber-600 dark:text-amber-400"
                                              : "text-gray-400"
                                        }`}
                                      />
                                    </div>
                                  )}
                                  <span className="text-xs font-medium">
                                    {policy.spec.resource.kind}
                                  </span>
                                </div>
                              </td>
                              <td className="px-3 py-2 border-b border-border last:border-b-0 text-center w-24">
                                <Badge
                                  variant={
                                    rules.audit > 0 ? "success" : "secondary"
                                  }
                                  className={
                                    rules.audit === 0
                                      ? "bg-gray-100 text-gray-400 dark:bg-gray-800 dark:text-gray-500"
                                      : ""
                                  }
                                >
                                  {rules.audit}
                                </Badge>
                              </td>
                              <td className="px-3 py-2 border-b border-border last:border-b-0 text-center w-24">
                                <Badge
                                  variant={
                                    rules.event > 0 ? "success" : "secondary"
                                  }
                                  className={
                                    rules.event === 0
                                      ? "bg-gray-100 text-gray-400 dark:bg-gray-800 dark:text-gray-500"
                                      : ""
                                  }
                                >
                                  {rules.event}
                                </Badge>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
