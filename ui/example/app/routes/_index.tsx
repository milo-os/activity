import { redirect, useNavigate, type LoaderFunctionArgs } from "react-router";
import { useState } from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  Input,
  Label,
  Button,
} from "@datum-cloud/activity-ui";

/**
 * Home/connection page.
 * In production: Redirects to /activity-feed
 * In development: Shows connection form to configure API URL
 */

export async function loader({ request: _request }: LoaderFunctionArgs) {
  // In production, redirect directly to activity feed
  if (process.env.NODE_ENV === "production") {
    return redirect("/activity-feed");
  }
  return null;
}

export default function Index() {
  const [apiUrl, setApiUrl] = useState("");
  const [token, setToken] = useState("");
  const navigate = useNavigate();

  const handleConnect = () => {
    // Store connection info in sessionStorage for the other pages to use
    sessionStorage.setItem("apiUrl", apiUrl);
    sessionStorage.setItem("token", token);
    navigate("/activity-feed");
  };

  return (
    <div className="min-h-screen flex flex-col bg-gradient-to-b from-muted to-muted/80">
      <header className="bg-transparent px-8 py-6 text-center">
        <div className="text-2xl font-semibold tracking-wider text-foreground">
          DATUM
        </div>
      </header>

      <Card className="max-w-[650px] mx-auto mt-8">
        <CardHeader>
          <CardTitle>Welcome</CardTitle>
          <CardDescription>
            Connect to Activity to start exploring audit logs and activities
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="space-y-2">
            <Label htmlFor="api-url">
              API Server URL{" "}
              <span className="font-normal text-muted-foreground">
                (usually proxied through your local machine)
              </span>
            </Label>
            <Input
              id="api-url"
              type="text"
              value={apiUrl}
              onChange={(e) => setApiUrl(e.target.value)}
              placeholder="http://localhost:6443"
              className="bg-muted"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="token">
              Bearer Token{" "}
              <span className="font-normal text-muted-foreground">
                (optional - leave blank if using client certificates)
              </span>
            </Label>
            <Input
              id="token"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Leave blank if not required"
              className="bg-muted"
            />
          </div>
          <Button onClick={handleConnect} className="w-full" size="lg">
            Connect to API
          </Button>

          <div className="pt-6 border-t">
            <h3 className="text-lg font-semibold text-foreground mb-1">
              What can you do with Activity Explorer?
            </h3>
            <p className="text-sm text-muted-foreground mb-4">
              Here are some common scenarios to get you started:
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="p-5 bg-muted border rounded-lg hover:border-primary hover:-translate-y-0.5 transition-all hover:bg-background hover:shadow-md">
                <h4 className="text-base font-semibold text-foreground mb-2">
                  Activity Feed
                </h4>
                <p className="text-sm text-muted-foreground mb-3">
                  Human-readable activity stream
                </p>
                <code className="block p-2 bg-background border rounded text-xs break-all text-foreground">
                  Filter by human vs system changes
                </code>
              </div>
              <div className="p-5 bg-muted border rounded-lg hover:border-primary hover:-translate-y-0.5 transition-all hover:bg-background hover:shadow-md">
                <h4 className="text-base font-semibold text-foreground mb-2">
                  Security Auditing
                </h4>
                <p className="text-sm text-muted-foreground mb-3">
                  Who's accessing your secrets?
                </p>
                <code className="block p-2 bg-background border rounded text-xs break-all text-foreground">
                  objectRef.resource == "secrets" && verb in ["get", "list"]
                </code>
              </div>
              <div className="p-5 bg-muted border rounded-lg hover:border-primary hover:-translate-y-0.5 transition-all hover:bg-background hover:shadow-md">
                <h4 className="text-base font-semibold text-foreground mb-2">
                  Compliance
                </h4>
                <p className="text-sm text-muted-foreground mb-3">
                  Track deletions in production
                </p>
                <code className="block p-2 bg-background border rounded text-xs break-all text-foreground">
                  audit.verb == "delete" && audit.objectRef.namespace == "production"
                </code>
              </div>
              <div className="p-5 bg-muted border rounded-lg hover:border-primary hover:-translate-y-0.5 transition-all hover:bg-background hover:shadow-md">
                <h4 className="text-base font-semibold text-foreground mb-2">
                  Troubleshooting
                </h4>
                <p className="text-sm text-muted-foreground mb-3">
                  Find failed pod operations
                </p>
                <code className="block p-2 bg-background border rounded text-xs break-all text-foreground">
                  {'objectRef.resource == "pods" && responseStatus.code >= 400'}
                </code>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <footer className="bg-gray-800 text-white px-8 py-8 text-center mt-auto">
        <p className="my-2">
          Powered by <strong>Activity</strong> (activity.miloapis.com/v1alpha1)
        </p>
        <p className="opacity-80 my-2">
          <a
            href="https://github.com/datum-cloud/activity"
            target="_blank"
            rel="noopener noreferrer"
            className="text-[#E6F59F] no-underline hover:underline"
          >
            GitHub
          </a>
          {" | "}
          <a
            href="/docs"
            target="_blank"
            rel="noopener noreferrer"
            className="text-[#E6F59F] no-underline hover:underline"
          >
            Documentation
          </a>
        </p>
      </footer>
    </div>
  );
}
