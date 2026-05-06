// Adapter shim: lets call sites continue to use the legacy compound
// <Tooltip><TooltipTrigger>X</TooltipTrigger><TooltipContent>Y</TooltipContent></Tooltip>
// pattern using Radix primitives directly.
//
// Why not route through @datum-cloud/datum-ui's <Tooltip message=…>:
// that public wrapper adds a `relative inline-flex` span around the
// trigger which sizes to content and breaks downstream truncation —
// `width: 100%` on a truncate span inside an inline-flex parent resolves
// to the parent's content width, not the surrounding layout's width.
// We use Radix directly with `asChild` to preserve the trigger element
// verbatim, then mirror datum-ui's content styling (secondary popover,
// arrow) so the visuals stay identical to the rest of the app.
import * as React from 'react';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import { cn } from '../../lib/utils';

const TooltipProvider = TooltipPrimitive.Provider;

const Tooltip = TooltipPrimitive.Root;

const TooltipTrigger = TooltipPrimitive.Trigger;

const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content> & {
    /** Optional override for the arrow's className. */
    arrowClassName?: string;
  }
>(({ className, sideOffset = 4, arrowClassName, children, ...props }, ref) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      // Mirrors @datum-cloud/datum-ui/tooltip's TooltipContent classes so
      // hovers look the same as everywhere else in the host app.
      className={cn(
        'tooltip-content',
        'bg-secondary text-secondary-foreground',
        'z-50 w-fit rounded-md px-3 py-1.5 text-xs text-balance',
        'max-w-[calc(100vw-2rem)]',
        'animate-in fade-in-0 zoom-in-95',
        'data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
        'data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2',
        className
      )}
      {...props}
    >
      {children}
      <TooltipPrimitive.Arrow
        className={cn(
          'fill-secondary -my-px border-none drop-shadow-[0_1px_0_secondary]',
          arrowClassName
        )}
        width={12}
        height={7}
        aria-hidden="true"
      />
    </TooltipPrimitive.Content>
  </TooltipPrimitive.Portal>
));
TooltipContent.displayName = TooltipPrimitive.Content.displayName;

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger };
