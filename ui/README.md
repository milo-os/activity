# Activity UI - React Components for Kubernetes Audit Logs

React component library for querying and visualizing Kubernetes audit logs via Activity (`activity.miloapis.com/v1alpha1`).

## Features

- 🔍 **FilterBuilder** - Interactive CEL expression builder for audit log queries
- 📊 **AuditEventViewer** - Rich visualization of audit events with expandable details
- 🎯 **AuditLogQueryComponent** - Complete query interface combining filter builder and results viewer
- ⚡ **useAuditLogQuery Hook** - React hook for programmatic query execution
- 🔌 **ActivityApiClient** - Typed API client for Activity
- 📦 **TypeScript Types** - Full type definitions matching the Kubernetes API schema

## Installation

```bash
npm install @miloapis/activity-ui
```

## Quick Start

```tsx
import {
  AuditLogQueryComponent,
  ActivityApiClient,
} from '@miloapis/activity-ui';
import '@miloapis/activity-ui/dist/styles.css';

function App() {
  const client = new ActivityApiClient({
    baseUrl: 'https://your-activity-api-server.com',
    token: 'your-bearer-token', // Optional
  });

  return (
    <AuditLogQueryComponent
      client={client}
      initialFilter='verb == "delete" && ns == "production"'
      initialLimit={100}
      onEventSelect={(event) => console.log('Selected:', event)}
    />
  );
}
```

## Components

### AuditLogQueryComponent

Complete query interface with filter builder and results viewer.

```tsx
<AuditLogQueryComponent
  client={client}
  initialFilter='resource == "secrets"'
  initialLimit={50}
  onEventSelect={(event) => {
    console.log('Event selected:', event);
  }}
  className="custom-class"
/>
```

**Props:**
- `client`: ActivityApiClient instance (required)
- `initialFilter`: Initial CEL filter expression (optional)
- `initialLimit`: Initial result limit (optional, default: 100)
- `onEventSelect`: Callback when an event is clicked (optional)
- `className`: Custom CSS class (optional)

### FilterBuilder

Interactive builder for CEL filter expressions.

```tsx
<FilterBuilder
  onFilterChange={(spec) => console.log('Filter:', spec)}
  initialFilter='verb == "delete"'
  initialLimit={100}
/>
```

**Props:**
- `onFilterChange`: Callback when filter/limit changes (required)
- `initialFilter`: Initial filter expression (optional)
- `initialLimit`: Initial limit value (optional)
- `className`: Custom CSS class (optional)

### AuditEventViewer

Display and interact with audit events.

```tsx
<AuditEventViewer
  events={auditEvents}
  onEventSelect={(event) => console.log(event)}
/>
```

**Props:**
- `events`: Array of audit events to display (required)
- `onEventSelect`: Callback when an event is clicked (optional)
- `className`: Custom CSS class (optional)

## Hooks

### useAuditLogQuery

React hook for executing queries programmatically.

```tsx
import { useAuditLogQuery, ActivityApiClient } from '@miloapis/activity-ui';

function MyComponent() {
  const client = new ActivityApiClient({ baseUrl: '...' });

  const {
    query,
    events,
    isLoading,
    error,
    hasMore,
    executeQuery,
    loadMore,
    reset,
  } = useAuditLogQuery({ client });

  const handleSearch = async () => {
    await executeQuery({
      filter: 'verb == "delete"',
      limit: 50,
    });
  };

  return (
    <div>
      <button onClick={handleSearch} disabled={isLoading}>
        Search
      </button>

      {events.map((event) => (
        <div key={event.auditID}>{event.verb} - {event.objectRef?.resource}</div>
      ))}

      {hasMore && (
        <button onClick={loadMore} disabled={isLoading}>
          Load More
        </button>
      )}
    </div>
  );
}
```

## API Client

### ActivityApiClient

Client for interacting with the Activity API server.

```tsx
import { ActivityApiClient } from '@miloapis/activity-ui';

const client = new ActivityApiClient({
  baseUrl: 'https://activity-api.example.com',
  token: 'your-token', // Optional
});

// Create a query
const query = await client.createQuery('my-query', {
  filter: 'verb == "delete" && ns == "production"',
  limit: 100,
});

// Get query results
const result = await client.getQuery('my-query');
console.log(result.status.results);

// Paginated query execution
for await (const page of client.executeQueryPaginated({
  filter: 'resource == "secrets"',
  limit: 100,
})) {
  console.log(`Page with ${page.status?.results?.length} events`);
}
```

## CEL Filter Examples

The filter field accepts CEL (Common Expression Language) expressions:

```javascript
// Find all delete operations
filter: 'verb == "delete"'

// Find operations in specific namespaces
filter: 'ns in ["production", "staging"]'

// Find secret access
filter: 'resource == "secrets" && verb in ["get", "list"]'

// Find operations by user
filter: 'user.startsWith("system:") && verb == "delete"'

// Time range filtering
filter: 'timestamp >= timestamp("2024-01-01T00:00:00Z") && timestamp <= timestamp("2024-12-31T23:59:59Z")'

// Complex queries
filter: 'verb == "delete" && resource in ["secrets", "configmaps"] && ns == "production" && stage == "ResponseComplete"'
```

### Available Filter Fields

- `timestamp` - Event timestamp (time.Time)
- `ns` - Kubernetes namespace (string)
- `verb` - HTTP verb (get, list, create, update, delete, etc.)
- `resource` - Resource type (pods, deployments, etc.)
- `user` - Username who performed the action
- `level` - Audit level (Metadata, Request, RequestResponse)
- `stage` - Event stage (RequestReceived, ResponseStarted, ResponseComplete, Panic)
- `uid` - Event UID
- `requestURI` - The request URI
- `sourceIPs` - Source IP addresses (array)

## Development

### Prerequisites

- Node.js 18+
- npm or yarn

### Building the Library

```bash
# Install dependencies
task ui:install

# Build the library
task ui:build

# Watch mode for development
task ui:dev

# Run type checking
task ui:type-check

# Run linter
task ui:lint
```

### Running the Example App

```bash
# Start the example application
task ui:start

# Or use the full command
task ui:example:dev
```

The example app will start at [http://localhost:3000](http://localhost:3000).

### Available Tasks

```bash
task ui:install          # Install dependencies
task ui:build            # Build component library
task ui:dev              # Build in watch mode
task ui:lint             # Lint code
task ui:type-check       # Type check
task ui:clean            # Clean build artifacts

# Example app
task ui:start            # Start example app (alias for example:dev)
task ui:example:dev      # Start example in dev mode
task ui:example:build    # Build example for production
task ui:example:preview  # Preview production build

# Combined
task ui:test             # Run lint + type-check
task ui:all              # Build library and example
```

## TypeScript Support

Full TypeScript support with exported types:

```tsx
import type {
  Event,
  AuditLogQuery,
  AuditLogQuerySpec,
  QueryPhase,
  FilterField,
} from '@miloapis/activity-ui';

const spec: AuditLogQuerySpec = {
  filter: 'verb == "delete"',
  limit: 100,
};

const handleEvent = (event: Event) => {
  console.log(event.verb, event.objectRef?.resource);
};
```

## Styling

Import the default styles:

```tsx
import '@miloapis/activity-ui/dist/styles.css';
```

Or customize by overriding CSS variables and classes. See the source CSS for available class names.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Run `task ui:test` to verify
6. Submit a pull request

## License

Apache-2.0

## Support

For issues and questions:
- GitHub Issues: https://github.com/datum-cloud/activity/issues
- Documentation: See the main [Activity README](../README.md)
