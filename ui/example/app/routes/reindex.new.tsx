import { useCallback } from "react";
import { useNavigate, useOutletContext, useSearchParams } from "react-router";
import { ReindexJobCreate, type ActivityApiClient } from "@datum-cloud/activity-ui";

interface OutletContext {
  client: ActivityApiClient;
}

/**
 * ReindexJob creation view - form for creating a new reindex job.
 */
export default function ReindexJobCreateView() {
  const { client } = useOutletContext<OutletContext>();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  // Get pre-selected policy from query params (e.g., ?policy=httpproxy-policy)
  const policyName = searchParams.get("policy") || undefined;

  const handleCreate = useCallback((jobName: string) => {
    // Navigate to the job detail view
    navigate(`/reindex/${encodeURIComponent(jobName)}`);
  }, [navigate]);

  const handleCancel = useCallback(() => {
    navigate("/reindex");
  }, [navigate]);

  return (
    <ReindexJobCreate
      client={client}
      policyName={policyName}
      onCreate={handleCreate}
      onCancel={handleCancel}
    />
  );
}
