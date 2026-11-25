import { useCallback } from "react";
import { useNavigate, useOutletContext, useParams } from "react-router";
import { ReindexJobDetailView, type ActivityApiClient } from "@datum-cloud/activity-ui";

interface OutletContext {
  client: ActivityApiClient;
}

/**
 * ReindexJob detail view - displays job progress and configuration with real-time updates.
 */
export default function ReindexJobDetail() {
  const { client } = useOutletContext<OutletContext>();
  const { name } = useParams();
  const navigate = useNavigate();

  const jobName = name ? decodeURIComponent(name) : undefined;

  const handleDelete = useCallback(() => {
    // Navigate back to the list after deletion
    navigate("/reindex");
  }, [navigate]);

  if (!jobName) {
    return <div>Job name is required</div>;
  }

  return (
    <ReindexJobDetailView
      client={client}
      jobName={jobName}
      onDelete={handleDelete}
    />
  );
}
