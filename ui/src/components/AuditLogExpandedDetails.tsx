import type { Event } from '../types';
import { TooltipProvider } from './ui/tooltip';
import { Timestamp } from './Timestamp';
import { DetailGrid, DetailPanelShell, Field, Section } from './details';

export interface AuditLogExpandedDetailsProps {
  /** The audit event to display details for */
  event: Event;
  /** When true, applies the in-table padded shell (default true) */
  compact?: boolean;
}

/**
 * AuditLogExpandedDetails renders the expanded details for an audit event.
 * Sections: Request / Response / When / User / Resource / Source. Advanced
 * fields and raw request/response objects are tucked under collapsed
 * <details> sections at the end.
 */
export function AuditLogExpandedDetails({ event, compact = true }: AuditLogExpandedDetailsProps) {
  const timestamp = event.stageTimestamp || event.requestReceivedTimestamp;
  const status = event.responseStatus;
  const isOk = status?.code != null && status.code >= 200 && status.code < 300;

  const body = (
    <>
      <DetailGrid>
        <Section title="Request">
          <Field
            label="Verb"
            value={event.verb || 'Unknown'}
            copyValue={event.verb || undefined}
            copyLabel="verb"
          />
          {event.requestURI ? (
            <Field label="URI" value={event.requestURI} copyValue={event.requestURI} mono />
          ) : null}
        </Section>

        {status ? (
          <Section title="Response">
            {status.code != null ? (
              <Field
                label="Status code"
                value={
                  <span
                    className={
                      isOk
                        ? 'text-green-600 dark:text-green-400'
                        : 'text-red-600 dark:text-red-400'
                    }
                  >
                    {isOk ? '✓ ' : '✗ '}
                    {status.code}
                  </span>
                }
                copyValue={String(status.code)}
                copyLabel="status code"
              />
            ) : null}
            {status.status ? <Field label="Status" value={status.status} /> : null}
            {status.reason ? <Field label="Reason" value={status.reason} /> : null}
            {status.message ? (
              <Field label="Message" value={status.message} copyValue={status.message} />
            ) : null}
          </Section>
        ) : null}

        <Section title="When">
          <Field
            label="Timestamp"
            value={<Timestamp value={timestamp} variant="iso-utc" />}
            copyValue={timestamp || ''}
            copyLabel="timestamp"
            disableTooltip
          />
        </Section>

        {event.user ? (
          <Section title="User">
            {event.user.username ? (
              <Field
                label="Username"
                value={event.user.username}
                copyValue={event.user.username}
              />
            ) : null}
            {event.user.uid ? (
              <Field label="UID" value={event.user.uid} copyValue={event.user.uid} mono />
            ) : null}
            {event.user.groups && event.user.groups.length > 0 ? (
              <Field label="Groups" value={event.user.groups.join(', ')} />
            ) : null}
          </Section>
        ) : null}

        {event.objectRef ? (
          <Section title="Resource">
            <Field
              label="Kind"
              value={
                <span>
                  {event.objectRef.resource || 'Unknown'}
                  {event.objectRef.apiGroup ? (
                    <span className="ml-1 text-muted-foreground">· {event.objectRef.apiGroup}</span>
                  ) : null}
                </span>
              }
              copyValue={event.objectRef.resource}
              copyLabel="resource kind"
            />
            {event.objectRef.name ? (
              <Field
                label="Name"
                value={event.objectRef.name}
                copyValue={event.objectRef.name}
                copyLabel="resource name"
              />
            ) : null}
            {event.objectRef.namespace ? (
              <Field
                label="Namespace"
                value={event.objectRef.namespace}
                copyValue={event.objectRef.namespace}
              />
            ) : null}
            {event.objectRef.apiVersion ? (
              <Field label="API version" value={event.objectRef.apiVersion} />
            ) : null}
            {event.objectRef.subresource ? (
              <Field label="Subresource" value={event.objectRef.subresource} />
            ) : null}
            {event.objectRef.uid ? (
              <Field label="UID" value={event.objectRef.uid} copyValue={event.objectRef.uid} mono />
            ) : null}
          </Section>
        ) : null}

        {event.userAgent || (event.sourceIPs && event.sourceIPs.length > 0) ? (
          <Section title="Source">
            {event.sourceIPs && event.sourceIPs.length > 0 ? (
              <Field label="Source IPs" value={event.sourceIPs.join(', ')} />
            ) : null}
            {event.userAgent ? (
              <Field
                label="User agent"
                value={event.userAgent}
                copyValue={event.userAgent}
                mono
              />
            ) : null}
          </Section>
        ) : null}
      </DetailGrid>

      {/* Advanced (collapsed) */}
      {event.auditID || event.stage || event.level ||
      (event.annotations && Object.keys(event.annotations).length > 0) ? (
        <details className="group mt-5">
          <summary className="cursor-pointer list-none">
            <h4 className="inline-flex items-center m-0 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/80 hover:text-foreground">
              <span className="mr-1 group-open:rotate-90 transition-transform">▸</span>
              Advanced
            </h4>
          </summary>
          <div className="mt-3 pl-3">
            <DetailGrid>
              <Section title="Audit">
                {event.auditID ? (
                  <Field label="Audit ID" value={event.auditID} copyValue={event.auditID} mono />
                ) : null}
                {event.stage ? <Field label="Stage" value={event.stage} /> : null}
                {event.level ? <Field label="Level" value={event.level} /> : null}
              </Section>
              {event.annotations && Object.keys(event.annotations).length > 0 ? (
                <Section title="Annotations">
                  {Object.entries(event.annotations).map(([key, value]) => (
                    <Field key={key} label={key} value={value} mono />
                  ))}
                </Section>
              ) : null}
            </DetailGrid>
          </div>
        </details>
      ) : null}

      {/* Raw request/response (collapsed) */}
      {event.requestObject || event.responseObject ? (
        <details className="group mt-3">
          <summary className="cursor-pointer list-none">
            <h4 className="inline-flex items-center m-0 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/80 hover:text-foreground">
              <span className="mr-1 group-open:rotate-90 transition-transform">▸</span>
              Raw objects
            </h4>
          </summary>
          <div className="mt-3 space-y-3">
            {event.requestObject ? (
              <div>
                <div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Request
                </div>
                <pre className="m-0 p-3 bg-muted rounded overflow-x-auto text-xs font-mono">
                  {JSON.stringify(event.requestObject, null, 2)}
                </pre>
              </div>
            ) : null}
            {event.responseObject ? (
              <div>
                <div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Response
                </div>
                <pre className="m-0 p-3 bg-muted rounded overflow-x-auto text-xs font-mono">
                  {JSON.stringify(event.responseObject, null, 2)}
                </pre>
              </div>
            ) : null}
          </div>
        </details>
      ) : null}
    </>
  );

  return (
    <TooltipProvider>
      {compact ? (
        <DetailPanelShell>{body}</DetailPanelShell>
      ) : (
        <div className="mt-4 pt-4 border-t border-border">{body}</div>
      )}
    </TooltipProvider>
  );
}
