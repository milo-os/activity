import { useState } from 'react';
import { Copy, Check } from 'lucide-react';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

/**
 * Small inline copy button. Fades in on `.group:hover` of the enclosing
 * Field so values look uncluttered until the user is interacting with
 * them. Used by all three feed detail panels.
 */
export function CopyButton({ value, label }: { value: string; label: string }) {
  const [isCopied, setIsCopied] = useState(false);
  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(value);
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), 1500);
    } catch (err) {
      console.error('Failed to copy:', err);
    }
  };
  return (
    <Tooltip delayDuration={400}>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={handleCopy}
          className="inline-flex items-center justify-center p-0.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-opacity cursor-pointer ml-1 opacity-0 group-hover:opacity-100"
          aria-label={`Copy ${label}`}
        >
          {isCopied ? (
            <Check className="h-3 w-3 text-green-600 dark:text-green-400" />
          ) : (
            <Copy className="h-3 w-3 text-gray-500 dark:text-gray-400" />
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent side="top">
        <p className="text-xs">{isCopied ? 'Copied!' : `Copy ${label}`}</p>
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * A label / value row inside a Section. The value is truncated to one
 * line and a hover tooltip surfaces the full text — keeps long UIDs,
 * resource names, etc. from blowing out the section's grid track.
 *
 * `value` may be any ReactNode for visible rendering; pass `copyValue`
 * (the plain string form) for both the copy button and the hover tooltip.
 * If `copyValue` isn't provided and `value` is a string, it doubles as
 * the tooltip body.
 */
export function Field({
  label,
  value,
  copyValue,
  copyLabel,
  mono = false,
  disableTooltip = false,
}: {
  label: string;
  value: React.ReactNode;
  copyValue?: string;
  copyLabel?: string;
  mono?: boolean;
  /**
   * Skip Field's hover tooltip. Use when the rendered `value` already
   * provides its own tooltip (e.g. <Timestamp>) so the two don't stack.
   */
  disableTooltip?: boolean;
}) {
  // Tooltip body — copyValue if given, else the value itself when string.
  const tooltipBody: React.ReactNode = disableTooltip
    ? null
    : copyValue ?? (typeof value === 'string' ? value : null);

  // Inner block that actually does the single-line truncation. `display:
  // block` + parent overflow:hidden + width:100% gives ellipsis a definite
  // width to clip against, which works regardless of flex/grid quirks
  // upstream. Tooltip wraps this inner block so hover reveals the full
  // value.
  const inner = (
    <span
      className={mono ? 'font-mono' : ''}
      style={{
        display: 'block',
        width: '100%',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
        cursor: tooltipBody ? 'help' : undefined,
      }}
    >
      {value}
    </span>
  );

  return (
    <div className="group flex flex-col gap-0.5 min-w-0" style={{ minWidth: 0 }}>
      <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </span>
      <div
        className="flex items-center gap-1 text-xs text-foreground"
        style={{ minWidth: 0 }}
      >
        {/* The flex item is the wrapper div with overflow:hidden so the
            inner block-level truncate span has a definite width. */}
        <div
          style={{
            flex: '1 1 auto',
            minWidth: 0,
            maxWidth: '100%',
            overflow: 'hidden',
          }}
        >
          {tooltipBody ? (
            <Tooltip>
              <TooltipTrigger asChild>{inner}</TooltipTrigger>
              <TooltipContent
                className="max-w-[480px] whitespace-normal break-words"
                side="top"
              >
                <span className={mono ? 'font-mono break-all' : 'break-words'}>
                  {tooltipBody}
                </span>
              </TooltipContent>
            </Tooltip>
          ) : (
            inner
          )}
        </div>
        {copyValue ? (
          <span style={{ flexShrink: 0 }}>
            <CopyButton value={copyValue} label={copyLabel ?? label.toLowerCase()} />
          </span>
        ) : null}
      </div>
    </div>
  );
}

/**
 * A grouped section in a detail panel: small uppercase title above a
 * vertical stack of Fields. `min-width: 0` is enforced inline so the
 * section can shrink below its content's natural width (otherwise long
 * unbroken values would force the parent grid track wider).
 */
export function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className="flex flex-col gap-2"
      style={{ minWidth: 0 }}
    >
      <h4 className="m-0 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground/80">
        {title}
      </h4>
      <div className="flex flex-col gap-2" style={{ minWidth: 0 }}>{children}</div>
    </div>
  );
}

/**
 * The wrapper a detail panel renders into. Sections lay out in a
 * responsive grid (each section claims at least 220px and at most an
 * equal share of available width). `minmax(0, 1fr)` is layered onto each
 * track so long content doesn't force tracks to expand past their share.
 */
export function DetailGrid({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="grid gap-x-8 gap-y-4"
      style={{
        gridTemplateColumns: 'repeat(auto-fit, minmax(min(220px, 100%), 1fr))',
        // `min(220px, 100%)` lets the track shrink below 220 on narrow
        // viewports; the inner Section + Field truncation keeps content
        // bounded once the track has its width.
      }}
    >
      {children}
    </div>
  );
}

/**
 * Standard padding/background for a detail panel row inside a Table.
 */
export function DetailPanelShell({ children }: { children: React.ReactNode }) {
  return <div className="px-6 py-5 bg-muted/30">{children}</div>;
}
