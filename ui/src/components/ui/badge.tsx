// Adapter: maps the legacy shadcn-style `variant` prop used by activity-ui
// call sites onto datum-ui's `type` + `theme` API.
import * as React from 'react';
import { Badge as DUIBadge, badgeVariants as duiBadgeVariants } from '@datum-cloud/datum-ui/badge';
import type { BadgeProps as DUIBadgeProps } from '@datum-cloud/datum-ui/badge';

type LegacyVariant =
  | 'default'
  | 'secondary'
  | 'destructive'
  | 'outline'
  | 'success'
  | 'warning';

export interface BadgeProps extends Omit<React.HTMLAttributes<HTMLDivElement>, 'children'> {
  variant?: LegacyVariant;
  children?: React.ReactNode;
}

function mapVariant(variant: LegacyVariant | undefined): {
  type: DUIBadgeProps['type'];
  theme: DUIBadgeProps['theme'];
} {
  switch (variant) {
    case 'destructive':
      return { type: 'danger', theme: 'solid' };
    case 'secondary':
      return { type: 'secondary', theme: 'solid' };
    case 'outline':
      return { type: 'muted', theme: 'outline' };
    case 'success':
      return { type: 'success', theme: 'light' };
    case 'warning':
      return { type: 'warning', theme: 'light' };
    case 'default':
    case undefined:
    default:
      return { type: 'primary', theme: 'solid' };
  }
}

function Badge({ variant, ...props }: BadgeProps) {
  const { type, theme } = mapVariant(variant);
  return <DUIBadge type={type} theme={theme} {...props} />;
}

const badgeVariants: typeof duiBadgeVariants = duiBadgeVariants;

export { Badge, badgeVariants };
