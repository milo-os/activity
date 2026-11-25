import { useNavigate, useOutletContext, useParams } from "react-router";
import { PolicyEditView, type ActivityApiClient, type ResourceRef } from "@datum-cloud/activity-ui";

interface OutletContext {
  client: ActivityApiClient;
}

/**
 * Edit existing policy view - shows Editor and Preview tabs.
 */
export default function PoliciesEdit() {
  const { client } = useOutletContext<OutletContext>();
  const { name } = useParams();
  const navigate = useNavigate();

  const policyName = name ? decodeURIComponent(name) : undefined;

  const handleSaveSuccess = (savedPolicyName: string) => {
    // Navigate back to detail view after successful save
    navigate(`/policies/${encodeURIComponent(savedPolicyName)}`);
  };

  const handleCancel = () => {
    // Navigate back to detail view on cancel
    if (policyName) {
      navigate(`/policies/${encodeURIComponent(policyName)}`);
    } else {
      navigate("/policies");
    }
  };

  const handleResourceClick = (resource: ResourceRef) => {
    alert(
      `Navigate to: ${resource.kind}/${resource.name} in namespace ${resource.namespace || "default"}`
    );
  };

  if (!policyName) {
    return <div>Policy name is required</div>;
  }

  return (
    <PolicyEditView
      client={client}
      policyName={policyName}
      onSaveSuccess={handleSaveSuccess}
      onCancel={handleCancel}
      onResourceClick={handleResourceClick}
    />
  );
}
