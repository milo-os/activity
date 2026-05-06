import type { Activity, TenantLinkResolver } from '../types/activity';
import { TooltipProvider } from './ui/tooltip';
import { Timestamp } from './Timestamp';
import { DetailGrid, DetailPanelShell, Field, Section } from './details';

export interface ActivityExpandedDetailsProps {
  /** The activity to display details for */
  activity: Activity;
  /** Optional resolver function to make tenant badges clickable */
  tenantLinkResolver?: TenantLinkResolver;
  /** When true, removes the top margin/border (caller handles the separator) */
  compact?: boolean;
}

/**
 * ActivityExpandedDetails renders the expanded details for an activity row.
 * Layout: an optional "Changes" block on top, then four grouped sections —
 * When / Actor / Resource / Origin — laid out as a responsive grid.
 */
export function ActivityExpandedDetails({
  activity,
  compact = false,
}: ActivityExpandedDetailsProps) {
  const { spec, metadata } = activity;
  const { actor, resource, origin, changes } = spec;
  const timestamp = metadata?.creationTimestamp;

  const actorDisplay = actor.displayName || actor.name;
  const actorIsUser = actor.type === 'user';

  const body = (
    <>
      {changes && changes.length > 0 ? (
        <div className="mb-4">
          <h4 className="m-0 mb-2 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/80">
            Changes
          </h4>
          <div className="flex flex-col gap-1.5">
            {changes.map((change, index) => (
              <div key={index} className="rounded bg-muted px-2.5 py-1.5 text-xs">
                <span className="block font-mono text-[11px] font-semibold text-foreground">
                  {change.field}
                </span>
                {change.old ? (
                  <span className="block ml-2 text-red-600 dark:text-red-400">
                    <span className="mr-1 font-medium">−</span>
                    <span className="line-through">{change.old}</span>
                  </span>
                ) : null}
                {change.new ? (
                  <span className="block ml-2 text-green-600 dark:text-green-400">
                    <span className="mr-1 font-medium">+</span>
                    {change.new}
                  </span>
                ) : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <DetailGrid>
        <Section title="When">
          <Field
            label="Timestamp"
            value={<Timestamp value={timestamp} variant="iso-utc" />}
            copyValue={timestamp ?? ''}
            copyLabel="timestamp"
            disableTooltip
          />
        </Section>

        <Section title="Actor">
          <Field
            label="Name"
            value={
              <span>
                {actorDisplay}
                {actor.type ? (
                  <span className="ml-1 text-muted-foreground">({actor.type})</span>
                ) : null}
              </span>
            }
            copyValue={actorDisplay}
            copyLabel="actor name"
          />
          {actorIsUser && actor.email ? (
            <Field label="Email" value={actor.email} copyValue={actor.email} mono />
          ) : null}
          {actor.uid ? (
            <Field label="UID" value={actor.uid} copyValue={actor.uid} mono />
          ) : null}
        </Section>

        <Section title="Resource">
          <Field
            label="Kind"
            value={
              <span>
                {resource.kind}
                {resource.apiGroup ? (
                  <span className="ml-1 text-muted-foreground">· {resource.apiGroup}</span>
                ) : null}
              </span>
            }
            copyValue={resource.kind}
            copyLabel="resource kind"
          />
          {resource.name ? (
            <Field
              label="Name"
              value={resource.name}
              copyValue={resource.name}
              copyLabel="resource name"
            />
          ) : null}
          {resource.namespace ? (
            <Field
              label="Namespace"
              value={resource.namespace}
              copyValue={resource.namespace}
            />
          ) : null}
          {resource.uid ? (
            <Field label="UID" value={resource.uid} copyValue={resource.uid} mono />
          ) : null}
        </Section>

        <Section title="Origin">
          <Field
            label="Source"
            value={origin.type}
            copyValue={origin.type}
            copyLabel="origin type"
          />
          {origin.id ? (
            <Field label="ID" value={origin.id} copyValue={origin.id} mono />
          ) : null}
        </Section>
      </DetailGrid>
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
