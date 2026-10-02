import { useState, useEffect } from "react";
import {
  SimpleQueryBuilder,
  ActivityApiClient,
  type AuditLogQuerySpec,
} from "@datum-cloud/activity-ui";
import { AppLayout } from "~/components/AppLayout";

/**
 * Query Builder page - mounts the standalone SimpleQueryBuilder so its facet
 * dropdowns can be exercised on their own. Not linked from the navigation.
 */
export default function QueryBuilderPage() {
  const [client, setClient] = useState<ActivityApiClient | null>(null);
  const [spec, setSpec] = useState<AuditLogQuerySpec | null>(null);

  useEffect(() => {
    const isProduction = typeof window !== "undefined" &&
      window.location.hostname !== "localhost" &&
      window.location.hostname !== "127.0.0.1";

    if (isProduction) {
      setClient(new ActivityApiClient({ baseUrl: "" }));
    } else {
      const apiUrl = sessionStorage.getItem("apiUrl") || "";
      const token = sessionStorage.getItem("token") || undefined;
      setClient(
        new ActivityApiClient({
          baseUrl: apiUrl || "",
          token,
        })
      );
    }
  }, []);

  return (
    <AppLayout>
      {client && (
        <SimpleQueryBuilder client={client} onFilterChange={setSpec} />
      )}
      {spec && (
        <pre className="text-xs text-muted-foreground" data-testid="query-spec">
          {JSON.stringify(spec, null, 2)}
        </pre>
      )}
    </AppLayout>
  );
}
