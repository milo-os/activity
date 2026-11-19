import { useNavigate, useOutletContext } from "react-router";
import { PolicyEditView, type ActivityApiClient, type ResourceRef } from "@datum-cloud/activity-ui";

interface OutletContext {
  client: ActivityApiClient;
}

/**
 * Create new policy view - shows Editor and Preview tabs.
 */
export default function PoliciesNew() {
  const { client } = useOutletContext<OutletContext>();
  const navigate = useNavigate();

  const handleSaveSuccess = (policyName: string) => {
    console.log("Policy created:", policyName);
    // Navigate to detail view after successful creation
    navigate(`/policies/${encodeURIComponent(policyName)}`);
  };

  const handleCancel = () => {
    navigate("/policies");
  };

  const handleResourceClick = (resource: ResourceRef) => {
    alert(
      `Navigate to: ${resource.kind}/${resource.name} in namespace ${resource.namespace || "default"}`
    );
  };

  return (
    <PolicyEditView
      client={client}
      onSaveSuccess={handleSaveSuccess}
      onCancel={handleCancel}
      onResourceClick={handleResourceClick}
    />
  );
}
