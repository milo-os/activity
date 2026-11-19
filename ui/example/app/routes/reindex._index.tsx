import { useCallback } from "react";
import { useNavigate, useOutletContext } from "react-router";
import { ReindexJobList, type ActivityApiClient } from "@datum-cloud/activity-ui";

interface OutletContext {
  client: ActivityApiClient;
}

/**
 * ReindexJob list view - displays all reindex jobs with real-time updates.
 */
export default function ReindexJobListView() {
  const { client } = useOutletContext<OutletContext>();
  const navigate = useNavigate();

  const handleViewJob = useCallback((jobName: string) => {
    navigate(`/reindex/${encodeURIComponent(jobName)}`);
  }, [navigate]);

  const handleCreateJob = useCallback(() => {
    navigate("/reindex/new");
  }, [navigate]);

  return (
    <ReindexJobList
      client={client}
      onViewJob={handleViewJob}
      onCreateJob={handleCreateJob}
    />
  );
}
