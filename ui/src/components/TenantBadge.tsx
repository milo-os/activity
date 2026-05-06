import type { Tenant, TenantLinkResolver, TenantType } from "../types/activity";
import { Badge } from "./ui/badge";
import { cn } from "../lib/utils";

export interface TenantBadgeProps {
  /** The tenant to display */
  tenant: Tenant;
  /** Optional resolver function to make the badge clickable */
  tenantLinkResolver?: TenantLinkResolver;
  /** Additional CSS class */
  className?: string;
  /** Size variant */
  size?: "default" | "compact";
}

/**
 * Get icon for tenant type
 */
function getTenantIcon(type: TenantType) {
  switch (type) {
    case "organization":
      return (
        <svg
          className="w-3 h-3"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4"
          />
        </svg>
      );
    case "project":
      return (
        <svg
          className="w-3 h-3"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"
          />
        </svg>
      );
    case "user":
      return (
        <svg
          className="w-3 h-3"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"
          />
        </svg>
      );
    case "platform":
      return (
        <svg
          className="w-3 h-3"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
          />
        </svg>
      );
    default:
      return null;
  }
}

/**
 * Get badge variant based on tenant type
 */
function getTenantBadgeVariant(
  type: TenantType,
): "default" | "secondary" | "outline" {
  switch (type) {
    case "organization":
      return "default";
    case "project":
      return "secondary";
    case "user":
      return "default";
    case "platform":
      return "default";
    default:
      return "secondary";
  }
}

/**
 * TenantBadge displays tenant information in a compact badge format
 * Renders as a clickable link if tenantLinkResolver is provided and returns a URL
 */
export function TenantBadge({
  tenant,
  tenantLinkResolver,
  className = "",
  size = "default",
}: TenantBadgeProps) {
  const icon = getTenantIcon(tenant.type);
  const variant = getTenantBadgeVariant(tenant.type);
  const url = tenantLinkResolver?.(tenant);

  const badgeContent = (
    <Badge
      variant={variant}
      className={cn(
        "inline-flex items-center gap-1 whitespace-nowrap",
        size === "compact" ? "text-xs h-4 py-0 px-1.5" : "text-xs h-5 px-2",
        url && "cursor-pointer hover:opacity-80 transition-opacity",
        className,
      )}
    >
      {icon}
      <span className="font-medium capitalize">{tenant.type}</span>
      {tenant.type !== "platform" && (
        <>
          <span className="text-muted-foreground">/</span>
          <span>{tenant.name}</span>
        </>
      )}
    </Badge>
  );

  if (url) {
    return (
      <a
        href={url}
        className="no-underline"
        onClick={(e) => e.stopPropagation()}
        title={`View ${tenant.type}: ${tenant.name}`}
      >
        {badgeContent}
      </a>
    );
  }

  return badgeContent;
}
