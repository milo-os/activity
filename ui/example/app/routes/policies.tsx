import { useState, useEffect } from "react";
import { Outlet } from "react-router";
import { ActivityApiClient } from "@datum-cloud/activity-ui";
import { AppLayout } from "~/components/AppLayout";

/**
 * Policy Management layout - provides shared header/footer and API client context.
 */
export default function PoliciesLayout() {
  const [client, setClient] = useState<ActivityApiClient | null>(null);

  useEffect(() => {
    // Check if in production environment
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
      <div>{client && <Outlet context={{ client }} />}</div>
    </AppLayout>
  );
}
