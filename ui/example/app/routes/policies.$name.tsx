import { useCallback } from "react";
import { useNavigate, useOutletContext, useParams } from "react-router";
import { PolicyDetailView, type ActivityApiClient, type ResourceRef } from "@datum-cloud/activity-ui";

interface OutletContext {
  client: ActivityApiClient;
}

/**
 * Policy detail view - displays read-only policy information with Activity/Events tabs.
 */
export default function PolicyDetail() {
  const { client } = useOutletContext<OutletContext>();
  const { name } = useParams();
  const navigate = useNavigate();

  const policyName = name ? decodeURIComponent(name) : undefined;

  const handleEdit = useCallback(() => {
    if (policyName) {
      navigate(`/policies/${encodeURIComponent(policyName)}/edit`);
    }
  }, [policyName, navigate]);

  const handleReindexSuccess = useCallback((jobName: string) => {
    // Optionally navigate to the job detail view
    navigate(`/reindex/${encodeURIComponent(jobName)}`);
  }, [navigate]);

  const handleResourceClick = (resource: ResourceRef) => {
    alert(
      `Navigate to: ${resource.kind}/${resource.name} in namespace ${resource.namespace || "default"}`
    );
  };

  if (!policyName) {
    return <div>Policy name is required</div>;
  }

  return (
    <PolicyDetailView
      client={client}
      policyName={policyName}
      onEdit={handleEdit}
      onReindexSuccess={handleReindexSuccess}
      onResourceClick={handleResourceClick}
    />
  );
}
