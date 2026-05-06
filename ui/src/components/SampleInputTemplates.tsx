import type { SampleInputTemplate, PolicyPreviewInput } from '../types/policy';
import type { Event } from '../types';

export interface SampleInputTemplatesProps {
  /** Current input type (audit or event) */
  inputType: 'audit' | 'event';
  /** Callback when a template is selected */
  onSelect: (input: PolicyPreviewInput) => void;
  /** Additional CSS class */
  className?: string;
}

/**
 * Pre-defined audit event templates
 */
const AUDIT_TEMPLATES: SampleInputTemplate[] = [
  {
    name: 'Create',
    description: 'Resource creation event',
    type: 'audit',
    input: {
      type: 'audit',
      audit: {
        level: 'RequestResponse',
        auditID: 'audit-create-001',
        stage: 'ResponseComplete',
        requestURI: '/apis/example.com/v1/namespaces/default/examples',
        verb: 'create',
        user: {
          username: 'alice@example.com',
          uid: 'user-alice-123',
          groups: ['users', 'developers'],
        },
        objectRef: {
          apiGroup: 'example.com',
          apiVersion: 'v1',
          resource: 'examples',
          namespace: 'default',
          name: 'my-resource',
          uid: 'res-456-789',
        },
        responseStatus: {
          code: 201,
          status: 'Success',
        },
        requestReceivedTimestamp: new Date().toISOString(),
        stageTimestamp: new Date().toISOString(),
      } as Event,
    },
  },
  {
    name: 'Update',
    description: 'Resource update event (PUT)',
    type: 'audit',
    input: {
      type: 'audit',
      audit: {
        level: 'RequestResponse',
        auditID: 'audit-update-001',
        stage: 'ResponseComplete',
        requestURI: '/apis/example.com/v1/namespaces/default/examples/my-resource',
        verb: 'update',
        user: {
          username: 'bob@example.com',
          uid: 'user-bob-456',
          groups: ['users', 'admins'],
        },
        objectRef: {
          apiGroup: 'example.com',
          apiVersion: 'v1',
          resource: 'examples',
          namespace: 'default',
          name: 'my-resource',
          uid: 'res-456-789',
        },
        responseStatus: {
          code: 200,
          status: 'Success',
        },
        requestReceivedTimestamp: new Date().toISOString(),
        stageTimestamp: new Date().toISOString(),
      } as Event,
    },
  },
  {
    name: 'Patch',
    description: 'Resource patch event',
    type: 'audit',
    input: {
      type: 'audit',
      audit: {
        level: 'RequestResponse',
        auditID: 'audit-patch-001',
        stage: 'ResponseComplete',
        requestURI: '/apis/example.com/v1/namespaces/default/examples/my-resource',
        verb: 'patch',
        user: {
          username: 'carol@example.com',
          uid: 'user-carol-789',
          groups: ['users'],
        },
        objectRef: {
          apiGroup: 'example.com',
          apiVersion: 'v1',
          resource: 'examples',
          namespace: 'default',
          name: 'my-resource',
          uid: 'res-456-789',
        },
        responseStatus: {
          code: 200,
          status: 'Success',
        },
        requestReceivedTimestamp: new Date().toISOString(),
        stageTimestamp: new Date().toISOString(),
      } as Event,
    },
  },
  {
    name: 'Delete',
    description: 'Resource deletion event',
    type: 'audit',
    input: {
      type: 'audit',
      audit: {
        level: 'RequestResponse',
        auditID: 'audit-delete-001',
        stage: 'ResponseComplete',
        requestURI: '/apis/example.com/v1/namespaces/default/examples/my-resource',
        verb: 'delete',
        user: {
          username: 'admin@example.com',
          uid: 'user-admin-000',
          groups: ['users', 'admins', 'cluster-admins'],
        },
        objectRef: {
          apiGroup: 'example.com',
          apiVersion: 'v1',
          resource: 'examples',
          namespace: 'default',
          name: 'my-resource',
          uid: 'res-456-789',
        },
        responseStatus: {
          code: 200,
          status: 'Success',
        },
        requestReceivedTimestamp: new Date().toISOString(),
        stageTimestamp: new Date().toISOString(),
      } as Event,
    },
  },
  {
    name: 'Status Update',
    description: 'Status subresource update',
    type: 'audit',
    input: {
      type: 'audit',
      audit: {
        level: 'RequestResponse',
        auditID: 'audit-status-001',
        stage: 'ResponseComplete',
        requestURI: '/apis/example.com/v1/namespaces/default/examples/my-resource/status',
        verb: 'update',
        user: {
          username: 'system:serviceaccount:kube-system:controller-manager',
          uid: 'sa-controller-123',
          groups: ['system:serviceaccounts', 'system:serviceaccounts:kube-system'],
        },
        objectRef: {
          apiGroup: 'example.com',
          apiVersion: 'v1',
          resource: 'examples',
          subresource: 'status',
          namespace: 'default',
          name: 'my-resource',
          uid: 'res-456-789',
        },
        responseStatus: {
          code: 200,
          status: 'Success',
        },
        requestReceivedTimestamp: new Date().toISOString(),
        stageTimestamp: new Date().toISOString(),
      } as Event,
    },
  },
];

/**
 * Pre-defined Kubernetes event templates
 */
const EVENT_TEMPLATES: SampleInputTemplate[] = [
  {
    name: 'Created',
    description: 'Resource created successfully',
    type: 'event',
    input: {
      type: 'event',
      event: {
        type: 'Normal',
        reason: 'Created',
        note: 'Successfully created resource',
        regarding: {
          apiVersion: 'example.com/v1',
          kind: 'Example',
          name: 'my-resource',
          namespace: 'default',
          uid: 'res-456-789',
        },
        reportingController: 'example-controller',
        eventTime: new Date().toISOString(),
        series: {
          count: 1,
          lastObservedTime: new Date().toISOString(),
        },
        metadata: {
          name: 'my-resource.abc123',
          namespace: 'default',
        },
      },
    },
  },
  {
    name: 'Ready',
    description: 'Resource became ready',
    type: 'event',
    input: {
      type: 'event',
      event: {
        type: 'Normal',
        reason: 'Ready',
        note: 'Resource is now ready to accept traffic',
        regarding: {
          apiVersion: 'example.com/v1',
          kind: 'Example',
          name: 'my-resource',
          namespace: 'default',
          uid: 'res-456-789',
        },
        reportingController: 'example-controller',
        eventTime: new Date().toISOString(),
        series: {
          count: 1,
          lastObservedTime: new Date().toISOString(),
        },
        metadata: {
          name: 'my-resource.def456',
          namespace: 'default',
        },
      },
    },
  },
  {
    name: 'Failed',
    description: 'Resource operation failed',
    type: 'event',
    input: {
      type: 'event',
      event: {
        type: 'Warning',
        reason: 'Failed',
        note: 'Failed to reconcile resource: timeout waiting for backend',
        regarding: {
          apiVersion: 'example.com/v1',
          kind: 'Example',
          name: 'my-resource',
          namespace: 'default',
          uid: 'res-456-789',
        },
        reportingController: 'example-controller',
        eventTime: new Date().toISOString(),
        series: {
          count: 3,
          lastObservedTime: new Date().toISOString(),
        },
        metadata: {
          name: 'my-resource.ghi789',
          namespace: 'default',
        },
      },
    },
  },
  {
    name: 'Progressing',
    description: 'Resource is progressing',
    type: 'event',
    input: {
      type: 'event',
      event: {
        type: 'Normal',
        reason: 'Progressing',
        note: 'Resource is being configured',
        regarding: {
          apiVersion: 'example.com/v1',
          kind: 'Example',
          name: 'my-resource',
          namespace: 'default',
          uid: 'res-456-789',
        },
        reportingController: 'example-controller',
        eventTime: new Date().toISOString(),
        series: {
          count: 1,
          lastObservedTime: new Date().toISOString(),
        },
        metadata: {
          name: 'my-resource.jkl012',
          namespace: 'default',
        },
      },
    },
  },
];

/**
 * SampleInputTemplates provides quick-fill buttons for common input patterns
 */
export function SampleInputTemplates({
  inputType,
  onSelect,
  className = '',
}: SampleInputTemplatesProps) {
  const templates = inputType === 'audit' ? AUDIT_TEMPLATES : EVENT_TEMPLATES;

  return (
    <div className={`mb-4 ${className}`}>
      <div className="text-xs font-medium text-muted-foreground mb-2">Quick Fill:</div>
      <div className="flex flex-wrap gap-1.5">
        {templates.map((template) => (
          <button
            key={template.name}
            type="button"
            className="px-2.5 py-1 bg-background border border-input rounded text-xs text-foreground transition-all duration-200 hover:bg-primary/10 hover:border-primary cursor-pointer"
            onClick={() => onSelect(template.input)}
            title={template.description}
          >
            {template.name}
          </button>
        ))}
      </div>
    </div>
  );
}

/**
 * Export templates for external use
 */
export { AUDIT_TEMPLATES, EVENT_TEMPLATES };
