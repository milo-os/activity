import React from 'react';
import { Tabs, TabsList, TabsTrigger } from '@datum-cloud/datum-ui/tabs';
import { cn } from '../lib/utils';

export interface ActivityTab {
  label: string;
  value: string;
  href: string;
}

export interface ActivityLayoutProps {
  /** Base path for constructing tab routes */
  basePath: string;
  /** Currently active tab value. Derive from the current URL in your app. */
  activeTab: string;
  /** Optional custom tabs. Defaults to Activity Feed, Events, and Audit Logs. */
  tabs?: ActivityTab[];
  /** Link component to render tab triggers as navigable links (e.g., react-router's Link) */
  linkComponent?: React.ElementType;
  /** Content to render below the tabs */
  children: React.ReactNode;
  /** Optional className for the outer container */
  className?: string;
}

const defaultTabs = (basePath: string): ActivityTab[] => [
  { label: 'Activity Feed', value: 'feed', href: basePath },
  { label: 'Events', value: 'events', href: `${basePath}/events` },
  { label: 'Audit Logs', value: 'audit-logs', href: `${basePath}/audit-logs` },
];

/**
 * Shared activity layout with tab navigation for Activity Feed, Events, and Audit Logs.
 * Framework-agnostic — pass a linkComponent (e.g., react-router's Link) for navigation.
 */
export function ActivityLayout({
  basePath,
  activeTab,
  tabs,
  linkComponent: LinkComp,
  children,
  className,
}: ActivityLayoutProps) {
  const resolvedTabs = tabs ?? defaultTabs(basePath);

  return (
    <div className={cn('flex h-full flex-col overflow-hidden', className)}>
      <div className="shrink-0 border-b px-4 pt-3">
        <Tabs value={activeTab}>
          <TabsList>
            {resolvedTabs.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value} asChild={!!LinkComp}>
                {LinkComp ? (
                  <LinkComp to={tab.href}>{tab.label}</LinkComp>
                ) : (
                  <span>{tab.label}</span>
                )}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden p-4">
        <div className="flex h-full flex-col">
          {children}
        </div>
      </div>
    </div>
  );
}
