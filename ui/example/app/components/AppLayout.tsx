import type { ReactNode } from "react";
import { Link, useLocation } from "react-router";
import { Github, BookOpen } from "lucide-react";

interface AppLayoutProps {
  children: ReactNode;
}

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
];

/**
 * Shared app layout with compact header (including navigation tabs) and main content area.
 * Optimized for maximum vertical space for content.
 */
export function AppLayout({ children }: AppLayoutProps) {
  const location = useLocation();

  const isActive = (tab: TabConfig) => {
    if (tab.matchPrefix) {
      return location.pathname.startsWith(tab.path);
    }
    return location.pathname === tab.path;
  };

  return (
    <div className="flex-1 min-h-0 flex flex-col bg-gradient-to-b from-muted to-muted/80 overflow-hidden">
      <header className="bg-transparent px-6 py-3 border-b border-border/40 shrink-0">
        <div className="flex items-center justify-between max-w-7xl mx-auto gap-6">
          <div className="flex-shrink-0">
            <img
              src="/logos/datum-logo-dark.svg"
              alt="Datum"
              className="h-4 hidden dark:block"
            />
            <img
              src="/logos/datum-logo-light.svg"
              alt="Datum"
              className="h-4 dark:hidden"
            />
          </div>

          <nav className="flex items-center gap-1 flex-1">
            {TABS.map((tab) => (
              <Link
                key={tab.path}
                to={tab.path}
                className={`px-2 py-1 text-xs font-medium transition-colors no-underline border-b-2 ${
                  isActive(tab)
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground hover:border-muted-foreground/40"
                }`}
              >
                {tab.label}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-3 text-muted-foreground flex-shrink-0">
            <a
              href="https://github.com/datum-cloud/activity"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-foreground transition-colors"
              title="GitHub"
            >
              <Github className="w-4 h-4" />
            </a>
            <a
              href="/docs"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-foreground transition-colors"
              title="Documentation"
            >
              <BookOpen className="w-4 h-4" />
            </a>
          </div>
        </div>
      </header>

      <main className="flex-1 min-h-0 px-6 py-4 max-w-7xl mx-auto w-full flex flex-col overflow-hidden">
        {children}
      </main>
    </div>
  );
}
