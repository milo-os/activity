import { useState, useEffect, useCallback } from 'react';
import type { ActivityPolicyResource } from '../types/policy';
import type { ActivityApiClient } from '../api/client';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@datum-cloud/datum-ui/select';
import { Input } from '@datum-cloud/datum-ui/input';
import { Label } from '@datum-cloud/datum-ui/label';
import { Button } from '@datum-cloud/datum-ui/button';

export interface PolicyResourceFormProps {
  /** Current resource configuration */
  resource: ActivityPolicyResource;
  /** Callback when resource changes */
  onChange: (resource: ActivityPolicyResource) => void;
  /** Optional API client for fetching discovered resources */
  client?: ActivityApiClient;
  /** Whether this is editing an existing policy (makes API group and kind read-only) */
  isEditMode?: boolean;
  /** Additional CSS class */
  className?: string;
}

/**
 * Convert plural resource name to Kind (PascalCase)
 * e.g., "httpproxies" -> "HTTPProxy", "gateways" -> "Gateway"
 */
function resourceToKind(resource: string): string {
  if (!resource) return '';
  // Remove trailing 's' or 'ies'
  let singular = resource;
  if (singular.endsWith('ies')) {
    singular = singular.slice(0, -3) + 'y';
  } else if (singular.endsWith('es')) {
    singular = singular.slice(0, -2);
  } else if (singular.endsWith('s')) {
    singular = singular.slice(0, -1);
  }
  // Convert to PascalCase
  return singular.charAt(0).toUpperCase() + singular.slice(1);
}

const CUSTOM_VALUE = '__custom__';

/**
 * PolicyResourceForm provides the form for editing policy resource configuration
 */
export function PolicyResourceForm({
  resource,
  onChange,
  client,
  isEditMode = false,
  className = '',
}: PolicyResourceFormProps) {
  // State for discovered API groups and resources
  const [apiGroups, setApiGroups] = useState<string[]>([]);
  const [resources, setResources] = useState<{ name: string; kind: string }[]>([]);
  const [isLoadingGroups, setIsLoadingGroups] = useState(false);
  const [isLoadingResources, setIsLoadingResources] = useState(false);

  // State for custom input mode
  const [customApiGroup, setCustomApiGroup] = useState(false);
  const [customKind, setCustomKind] = useState(false);


  // Load API groups that have audit events
  const loadApiGroups = useCallback(async () => {
    if (!client) return;
    setIsLoadingGroups(true);
    try {
      const groups = await client.getAuditedAPIGroups();
      setApiGroups(groups.filter((g) => g)); // Filter out empty strings
    } catch (err) {
      console.error('Failed to load API groups:', err);
    } finally {
      setIsLoadingGroups(false);
    }
  }, [client]);

  // Load resources for the selected API group
  const loadResources = useCallback(async () => {
    if (!client || !resource.apiGroup) {
      setResources([]);
      return;
    }
    setIsLoadingResources(true);
    try {
      // Get audited resource names from audit logs
      const auditedResources = await client.getAuditedResources(resource.apiGroup);

      // Get the actual Kind from Kubernetes API discovery
      let resourceMap = new Map<string, string>();
      try {
        const discoveryResult = await client.discoverAPIResources(resource.apiGroup);
        resourceMap = new Map(
          discoveryResult.resources?.map((r) => [r.name, r.kind]) || []
        );
      } catch {
        // API discovery not available - will use derived Kind names
      }

      // Combine audited resources with Kind names (from discovery or derived)
      const resourcesWithKind = auditedResources
        .filter((r) => r)
        .map((r) => ({
          name: r,
          kind: resourceMap.get(r) || resourceToKind(r),
        }));

      setResources(resourcesWithKind);
    } catch (err) {
      console.error('Failed to load resources:', err);
    } finally {
      setIsLoadingResources(false);
    }
  }, [client, resource.apiGroup]);

  // Load API groups on mount if client is available
  useEffect(() => {
    if (client) {
      loadApiGroups();
    }
  }, [client, loadApiGroups]);

  // Load resources when API group changes
  useEffect(() => {
    if (client && resource.apiGroup) {
      loadResources();
    }
  }, [client, resource.apiGroup, loadResources]);

  // Check if current value is in the list (for determining if custom mode is needed)
  useEffect(() => {
    if (resource.apiGroup && apiGroups.length > 0 && !apiGroups.includes(resource.apiGroup)) {
      setCustomApiGroup(true);
    }
  }, [resource.apiGroup, apiGroups]);

  useEffect(() => {
    if (resource.kind && resources.length > 0) {
      // Check if the kind matches any resource
      const kindInList = resources.some((r) => r.kind === resource.kind);
      if (!kindInList) {
        setCustomKind(true);
      }
    }
  }, [resource.kind, resources]);

  const handleApiGroupSelectChange = (value: string) => {
    if (value === CUSTOM_VALUE) {
      setCustomApiGroup(true);
    } else {
      setCustomApiGroup(false);
      onChange({ ...resource, apiGroup: value, kind: '' }); // Clear kind when group changes
    }
  };

  const handleApiGroupInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    onChange({ ...resource, apiGroup: e.target.value, kind: '' }); // Clear kind when group changes
  };

  const handleKindSelectChange = (value: string) => {
    if (value === CUSTOM_VALUE) {
      setCustomKind(true);
    } else {
      setCustomKind(false);
      // Find the resource and use its actual Kind from API discovery
      const selectedResource = resources.find((r) => r.name === value);
      const kind = selectedResource?.kind || resourceToKind(value);
      onChange({ ...resource, kind });
    }
  };

  const handleKindInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    onChange({ ...resource, kind: e.target.value });
  };

  const handleBackToSelect = (field: 'apiGroup' | 'kind') => {
    if (field === 'apiGroup') {
      setCustomApiGroup(false);
      onChange({ ...resource, apiGroup: '', kind: '' });
    } else {
      setCustomKind(false);
      onChange({ ...resource, kind: '' });
    }
  };

  // Check if current apiGroup is in the list
  const apiGroupInList = apiGroups.includes(resource.apiGroup);

  // Find matching resource for current kind
  const currentResourceName = resources.find((r) => r.kind === resource.kind)?.name || '';

  // Hide entire section in edit mode since these fields are already shown in the header
  if (isEditMode) {
    return null;
  }

  return (
    <div className={`rounded-lg bg-muted p-6 ${className}`}>
      <h4 className="mb-2 text-base font-medium text-foreground">Resource Target</h4>
      <p className="mb-6 text-sm text-muted-foreground">
        Define which API group and kind this policy applies to.
        {client && (
          <span className="italic text-muted-foreground/70">
            {' '}Options based on audit events in your cluster.
          </span>
        )}
      </p>

      {/* API Group */}
      <div className="mb-5 last:mb-0">
        <Label htmlFor="resource-apiGroup" className="mb-1.5 block text-foreground/80">
          API Group <span className="text-destructive">*</span>
        </Label>
        {client && !customApiGroup ? (
          <div className="relative">
            <Select
              value={apiGroupInList ? resource.apiGroup : ''}
              onValueChange={handleApiGroupSelectChange}
              disabled={isLoadingGroups}
            >
              <SelectTrigger className="h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm transition-all duration-200 focus:border-[#BF9595] focus:outline-none focus:ring-[3px] focus:ring-[#BF9595]/10 disabled:cursor-not-allowed disabled:opacity-50">
                <SelectValue placeholder={isLoadingGroups ? "Loading..." : "Select an API Group..."} />
              </SelectTrigger>
              <SelectContent>
                {apiGroups.length === 0 && !isLoadingGroups && (
                  <SelectItem value={CUSTOM_VALUE} disabled className="italic text-muted-foreground">
                    No API groups found
                  </SelectItem>
                )}
                {apiGroups.map((group) => (
                  <SelectItem key={group} value={group}>
                    {group}
                  </SelectItem>
                ))}
                <SelectItem value={CUSTOM_VALUE} className="italic text-muted-foreground">
                  Enter custom value...
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : (
          <div className="flex gap-2">
            <Input
              id="resource-apiGroup"
              type="text"
              className="flex-1"
              value={resource.apiGroup}
              onChange={handleApiGroupInputChange}
              placeholder="e.g., networking.datumapis.com"
            />
            {client && (
              <Button
                htmlType="button"
                type="tertiary" theme="outline"
                size="small"
                onClick={() => handleBackToSelect('apiGroup')}
                title="Back to select"
              >
                Back to list
              </Button>
            )}
          </div>
        )}
      </div>

      {/* Kind */}
      <div className="mb-5 last:mb-0">
        <Label htmlFor="resource-kind" className="mb-1.5 block text-foreground/80">
          Kind <span className="text-destructive">*</span>
        </Label>
        {client && !customKind ? (
          <div className="relative">
            <Select
              value={currentResourceName}
              onValueChange={handleKindSelectChange}
              disabled={isLoadingResources || !resource.apiGroup}
            >
              <SelectTrigger className="h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm transition-all duration-200 focus:border-[#BF9595] focus:outline-none focus:ring-[#BF9595]/10 disabled:cursor-not-allowed disabled:opacity-50">
                <SelectValue
                  placeholder={
                    !resource.apiGroup
                      ? 'Select API Group first...'
                      : isLoadingResources
                      ? 'Loading...'
                      : 'Select a Kind...'
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {resources.length === 0 && resource.apiGroup && !isLoadingResources && (
                  <SelectItem value={CUSTOM_VALUE} disabled className="italic text-muted-foreground">
                    No resources found
                  </SelectItem>
                )}
                {resources.map((res) => (
                  <SelectItem key={res.name} value={res.name}>
                    {res.kind}
                  </SelectItem>
                ))}
                <SelectItem value={CUSTOM_VALUE} className="italic text-muted-foreground">
                  Enter custom value...
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : (
          <div className="flex gap-2">
            <Input
              id="resource-kind"
              type="text"
              className="flex-1"
              value={resource.kind}
              onChange={handleKindInputChange}
              placeholder={resource.apiGroup ? 'e.g., HTTPProxy' : 'Select API Group first'}
              disabled={!resource.apiGroup}
            />
            {client && (
              <Button
                htmlType="button"
                type="tertiary" theme="outline"
                size="small"
                onClick={() => handleBackToSelect('kind')}
                title="Back to select"
              >
                Back to list
              </Button>
            )}
          </div>
        )}
        <div className="mt-1.5 text-xs text-muted-foreground">
          The Kubernetes resource kind (e.g., HTTPProxy, Gateway, Deployment)
        </div>
      </div>
    </div>
  );
}
