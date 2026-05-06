import { useState } from "react";
import { format } from "date-fns";
import type { Event } from "../types";
import type { Tenant, TenantLinkResolver, TenantType } from "../types/activity";
import { TenantBadge } from "./TenantBadge";
import { Button } from "@datum-cloud/datum-ui/button";
import { Card } from "@datum-cloud/datum-ui/card";
import { Badge } from "./ui/badge";

export interface AuditEventViewerProps {
  events: Event[];
  className?: string;
  onEventSelect?: (event: Event) => void;
  /** Optional resolver function to make tenant badges clickable */
  tenantLinkResolver?: TenantLinkResolver;
}

/**
 * Extract tenant information from audit event annotations if present
 * Expected annotations: tenant.type and tenant.name
 */
function extractTenantFromAnnotations(event: Event): Tenant | undefined {
  const annotations = event.annotations;
  if (!annotations) return undefined;

  const tenantType = annotations["tenant.type"];
  const tenantName = annotations["tenant.name"];

  if (tenantType && tenantName) {
    const validTypes: TenantType[] = [
      "platform",
      "organization",
      "project",
      "user",
    ];
    if (validTypes.includes(tenantType as TenantType)) {
      return {
        type: tenantType as Tenant["type"],
        name: tenantName,
      };
    }
  }

  return undefined;
}

/**
 * AuditEventViewer displays a list of audit events with details
 */
export function AuditEventViewer({
  events,
  className = "",
  onEventSelect,
  tenantLinkResolver,
}: AuditEventViewerProps) {
  const [selectedEvent, setSelectedEvent] = useState<Event | null>(null);
  const [expandedEvents, setExpandedEvents] = useState<Set<string>>(new Set());

  const toggleEventExpansion = (auditId: string) => {
    const newExpanded = new Set(expandedEvents);
    if (expandedEvents.has(auditId)) {
      newExpanded.delete(auditId);
    } else {
      newExpanded.add(auditId);
    }
    setExpandedEvents(newExpanded);
  };

  const handleEventClick = (event: Event) => {
    setSelectedEvent(event);
    if (onEventSelect) {
      onEventSelect(event);
    }
  };

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "N/A";
    try {
      return format(new Date(timestamp), "yyyy-MM-dd HH:mm:ss");
    } catch {
      return timestamp;
    }
  };

  const getVerbBadgeVariant = (
    verb?: string,
  ):
    | "default"
    | "secondary"
    | "destructive"
    | "outline"
    | "success"
    | "warning" => {
    switch (verb?.toLowerCase()) {
      case "create":
        return "success";
      case "update":
      case "patch":
        return "warning";
      case "delete":
        return "destructive";
      case "get":
      case "list":
      case "watch":
        return "default";
      default:
        return "secondary";
    }
  };

  if (events.length === 0) {
    return (
      <div className={`bg-muted rounded-lg border border-border ${className}`}>
        <div className="p-12 text-center text-muted-foreground text-sm">
          No events found
        </div>
      </div>
    );
  }

  return (
    <div className={`bg-muted rounded-lg border border-border ${className}`}>
      <div className="p-4">
        {events.map((event) => {
          const auditId = event.auditID || "";
          const isExpanded = expandedEvents.has(auditId);
          const tenant = extractTenantFromAnnotations(event);

          return (
            <Card
              key={auditId}
              className={`p-5 mb-3 cursor-pointer transition-all hover:border-primary/50 hover:shadow-sm hover:-translate-y-px ${
                selectedEvent?.auditID === auditId
                  ? "border-primary bg-primary/5 shadow-md"
                  : ""
              }`}
              onClick={() => handleEventClick(event)}
            >
              <div className="flex justify-between items-center">
                <div className="flex gap-3 items-center flex-1">
                  <Badge
                    variant={getVerbBadgeVariant(event.verb)}
                    className="px-3 py-1"
                  >
                    {event.verb?.toUpperCase() || "UNKNOWN"}
                  </Badge>
                  <span className="font-semibold">
                    {event.objectRef?.resource || "N/A"}
                  </span>
                  {event.objectRef?.namespace && (
                    <span className="text-muted-foreground text-sm">
                      ns: {event.objectRef.namespace}
                    </span>
                  )}
                  {event.objectRef?.name && (
                    <span className="text-foreground/80 text-sm">
                      {event.objectRef.name}
                    </span>
                  )}
                  {tenant && (
                    <TenantBadge
                      tenant={tenant}
                      tenantLinkResolver={tenantLinkResolver}
                      size="compact"
                    />
                  )}
                </div>
                <div className="flex gap-4 items-center">
                  <span className="text-foreground/80 text-sm">
                    {event.user?.username || "N/A"}
                  </span>
                  <span className="text-muted-foreground text-sm font-mono">
                    {formatTimestamp(event.stageTimestamp)}
                  </span>
                  <Button
                    type="quaternary" theme="borderless"
                    size="small"
                    onClick={(e) => {
                      e.stopPropagation();
                      toggleEventExpansion(auditId);
                    }}
                    className="text-primary"
                  >
                    {isExpanded ? "▼" : "▶"}
                  </Button>
                </div>
              </div>

              {isExpanded && (
                <div className="mt-4 pt-4 border-t border-border">
                  <div className="mb-4">
                    <h4 className="mt-0 mb-2 text-base text-foreground/80">
                      Event Information
                    </h4>
                    <dl className="grid grid-cols-[auto_1fr] gap-2 m-0">
                      <dt className="font-semibold text-foreground/80">
                        Audit ID:
                      </dt>
                      <dd className="m-0 text-foreground">
                        {event.auditID || "N/A"}
                      </dd>
                      <dt className="font-semibold text-foreground/80">
                        Stage:
                      </dt>
                      <dd className="m-0 text-foreground">
                        {event.stage || "N/A"}
                      </dd>
                      <dt className="font-semibold text-foreground/80">
                        Level:
                      </dt>
                      <dd className="m-0 text-foreground">
                        {event.level || "N/A"}
                      </dd>
                      <dt className="font-semibold text-foreground/80">
                        Request URI:
                      </dt>
                      <dd className="m-0 text-foreground font-mono text-sm break-all">
                        {event.requestURI || "N/A"}
                      </dd>
                      {event.userAgent && (
                        <>
                          <dt className="font-semibold text-foreground/80">
                            User Agent:
                          </dt>
                          <dd className="m-0 text-foreground font-mono text-sm break-all">
                            {event.userAgent}
                          </dd>
                        </>
                      )}
                      {event.sourceIPs && event.sourceIPs.length > 0 && (
                        <>
                          <dt className="font-semibold text-foreground/80">
                            Source IPs:
                          </dt>
                          <dd className="m-0 text-foreground">
                            {event.sourceIPs.join(", ")}
                          </dd>
                        </>
                      )}
                    </dl>
                  </div>

                  {tenant && (
                    <div className="mb-4">
                      <h4 className="mt-0 mb-2 text-base text-foreground/80">
                        Tenant
                      </h4>
                      <TenantBadge
                        tenant={tenant}
                        tenantLinkResolver={tenantLinkResolver}
                      />
                    </div>
                  )}

                  {event.user && (
                    <div className="mb-4">
                      <h4 className="mt-0 mb-2 text-base text-foreground/80">
                        User Information
                      </h4>
                      <dl className="grid grid-cols-[auto_1fr] gap-2 m-0">
                        <dt className="font-semibold text-foreground/80">
                          Username:
                        </dt>
                        <dd className="m-0 text-foreground">
                          {event.user.username || "N/A"}
                        </dd>
                        <dt className="font-semibold text-foreground/80">
                          UID:
                        </dt>
                        <dd className="m-0 text-foreground">
                          {event.user.uid || "N/A"}
                        </dd>
                        {event.user.groups && event.user.groups.length > 0 && (
                          <>
                            <dt className="font-semibold text-foreground/80">
                              Groups:
                            </dt>
                            <dd className="m-0 text-foreground">
                              {event.user.groups.join(", ")}
                            </dd>
                          </>
                        )}
                      </dl>
                    </div>
                  )}

                  {event.responseStatus && (
                    <div className="mb-4">
                      <h4 className="mt-0 mb-2 text-base text-foreground/80">
                        Response Status
                      </h4>
                      <dl className="grid grid-cols-[auto_1fr] gap-2 m-0">
                        <dt className="font-semibold text-foreground/80">
                          Code:
                        </dt>
                        <dd className="m-0 text-foreground">
                          {event.responseStatus.code || "N/A"}
                        </dd>
                        <dt className="font-semibold text-foreground/80">
                          Status:
                        </dt>
                        <dd className="m-0 text-foreground">
                          {event.responseStatus.status || "N/A"}
                        </dd>
                        {event.responseStatus.message && (
                          <>
                            <dt className="font-semibold text-foreground/80">
                              Message:
                            </dt>
                            <dd className="m-0 text-foreground">
                              {event.responseStatus.message}
                            </dd>
                          </>
                        )}
                      </dl>
                    </div>
                  )}

                  {event.annotations &&
                    Object.keys(event.annotations).length > 0 && (
                      <div className="mb-4">
                        <h4 className="mt-0 mb-2 text-base text-foreground/80">
                          Annotations
                        </h4>
                        <dl className="grid grid-cols-[auto_1fr] gap-2 m-0">
                          {Object.entries(event.annotations).map(
                            ([key, value]) => (
                              <div key={key} className="contents">
                                <dt className="font-semibold text-foreground/80">
                                  {key}:
                                </dt>
                                <dd className="m-0 text-foreground">{value}</dd>
                              </div>
                            ),
                          )}
                        </dl>
                      </div>
                    )}

                  {event.requestObject || event.responseObject ? (
                    <div className="mb-4">
                      <h4 className="mt-0 mb-2 text-base text-foreground/80">
                        Request/Response Data
                      </h4>
                      {event.requestObject ? (
                        <details className="mt-2">
                          <summary className="cursor-pointer font-semibold p-2 bg-muted rounded">
                            Request Object
                          </summary>
                          <pre className="mt-2 p-4 bg-muted rounded overflow-x-auto text-sm">
                            {JSON.stringify(event.requestObject, null, 2)}
                          </pre>
                        </details>
                      ) : null}
                      {event.responseObject ? (
                        <details className="mt-2">
                          <summary className="cursor-pointer font-semibold p-2 bg-muted rounded">
                            Response Object
                          </summary>
                          <pre className="mt-2 p-4 bg-muted rounded overflow-x-auto text-sm">
                            {JSON.stringify(event.responseObject, null, 2)}
                          </pre>
                        </details>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              )}
            </Card>
          );
        })}
      </div>
    </div>
  );
}
