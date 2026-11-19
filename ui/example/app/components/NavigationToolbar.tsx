import { Link, useLocation } from "react-router";

type TabConfig = {
  path: string;
  label: string;
  matchPrefix?: boolean;
};

const TABS: TabConfig[] = [
  { path: "/activity-feed", label: "Activity Feed" },
  { path: "/events", label: "Events" },
  { path: "/audit-logs", label: "Audit Logs" },
  { path: "/resource-history", label: "Resource History" },
  { path: "/policies", label: "Manage Policies", matchPrefix: true },
  { path: "/reindex", label: "Reindex Jobs", matchPrefix: true },
];

/**
 * Compact navigation toolbar with tabs.
 * Optimized for minimal vertical space while maintaining usability.
 */
export function NavigationToolbar() {
  const location = useLocation();

  const isActive = (tab: TabConfig) => {
    if (tab.matchPrefix) {
      return location.pathname.startsWith(tab.path);
    }
    return location.pathname === tab.path;
  };

  return (
    <nav className="flex items-center mb-4 pb-2 border-b border-border/60">
      <div className="flex gap-1">
        {TABS.map((tab) => (
          <Link
            key={tab.path}
            to={tab.path}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-all no-underline ${
              isActive(tab)
                ? "bg-primary text-primary-foreground shadow-sm"
                : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
            }`}
          >
            {tab.label}
          </Link>
        ))}
      </div>
    </nav>
  );
}
