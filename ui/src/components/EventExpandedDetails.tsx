import type { K8sEvent } from '../types/k8s-event';
import { TooltipProvider } from './ui/tooltip';
import { Timestamp } from './Timestamp';
import { DetailGrid, DetailPanelShell, Field, Section } from './details';

export interface EventExpandedDetailsProps {
  /** The event to display details for */
  event: K8sEvent;
  /** When true, applies the in-table padded shell (default true) */
  compact?: boolean;
}

function getRegarding(event: K8sEvent) {
  return event.regarding || event.involvedObject || {};
}
function getReportingController(event: K8sEvent): string | undefined {
  return event.reportingController || event.reportingComponent || event.source?.component;
}
function getReportingInstance(event: K8sEvent): string | undefined {
  return event.reportingInstance || event.source?.host;
}

/**
 * EventExpandedDetails renders the expanded details for a Kubernetes
 * event row. Sections: Object / When / Reporter / Action / Related /
 * Metadata. Action is only shown if present.
 */
export function EventExpandedDetails({ event, compact = true }: EventExpandedDetailsProps) {
  const regarding = getRegarding(event);
  const reportingController = getReportingController(event);
  const reportingInstance = getReportingInstance(event);
  const { eventTime, action, metadata, related } = event;

  const firstTimestamp = event.firstTimestamp || event.deprecatedFirstTimestamp;
  const lastTimestamp =
    event.lastTimestamp ||
    event.deprecatedLastTimestamp ||
    event.series?.lastObservedTime;
  const count = event.series?.count || event.count || event.deprecatedCount;

  const body = (
    <DetailGrid>
      <Section title="Object">
        <Field
          label="Kind"
          value={
            <span>
              {regarding.kind || 'Unknown'}
              {regarding.apiVersion ? (
                <span className="ml-1 text-muted-foreground">· {regarding.apiVersion}</span>
              ) : null}
            </span>
          }
          copyValue={regarding.kind}
          copyLabel="object kind"
        />
        <Field
          label="Name"
          value={regarding.name || 'Unknown'}
          copyValue={regarding.name}
          copyLabel="object name"
        />
        {regarding.namespace ? (
          <Field
            label="Namespace"
            value={regarding.namespace}
            copyValue={regarding.namespace}
          />
        ) : null}
        {regarding.uid ? (
          <Field label="UID" value={regarding.uid} copyValue={regarding.uid} mono />
        ) : null}
        {regarding.fieldPath ? (
          <Field
            label="Field path"
            value={regarding.fieldPath}
            copyValue={regarding.fieldPath}
            mono
          />
        ) : null}
      </Section>

      <Section title="When">
        {eventTime ? (
          <Field
            label="Event time"
            value={<Timestamp value={eventTime} variant="iso-utc" />}
            copyValue={eventTime}
            copyLabel="event time"
            disableTooltip
          />
        ) : null}
        {firstTimestamp ? (
          <Field
            label="First seen"
            value={<Timestamp value={firstTimestamp} variant="iso-utc" />}
            copyValue={firstTimestamp}
            copyLabel="first seen"
            disableTooltip
          />
        ) : null}
        {lastTimestamp ? (
          <Field
            label="Last seen"
            value={<Timestamp value={lastTimestamp} variant="iso-utc" />}
            copyValue={lastTimestamp}
            copyLabel="last seen"
            disableTooltip
          />
        ) : null}
        {count && count > 1 ? (
          <Field label="Count" value={`${count} times`} />
        ) : null}
      </Section>

      {reportingController || reportingInstance ? (
        <Section title="Reporter">
          {reportingController ? (
            <Field
              label="Controller"
              value={reportingController}
              copyValue={reportingController}
            />
          ) : null}
          {reportingInstance ? (
            <Field label="Instance" value={reportingInstance} mono />
          ) : null}
        </Section>
      ) : null}

      {action ? (
        <Section title="Action">
          <Field label="Action" value={action} copyValue={action} />
        </Section>
      ) : null}

      {related ? (
        <Section title="Related">
          {related.kind ? <Field label="Kind" value={related.kind} /> : null}
          {related.name ? (
            <Field label="Name" value={related.name} copyValue={related.name} />
          ) : null}
          {related.namespace ? (
            <Field
              label="Namespace"
              value={related.namespace}
              copyValue={related.namespace}
            />
          ) : null}
        </Section>
      ) : null}

      {metadata ? (
        <Section title="Metadata">
          {metadata.name ? (
            <Field label="Name" value={metadata.name} copyValue={metadata.name} mono />
          ) : null}
          {metadata.uid ? (
            <Field label="UID" value={metadata.uid} copyValue={metadata.uid} mono />
          ) : null}
          {metadata.resourceVersion ? (
            <Field
              label="Resource version"
              value={metadata.resourceVersion}
              copyValue={metadata.resourceVersion}
              mono
            />
          ) : null}
        </Section>
      ) : null}
    </DetailGrid>
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
