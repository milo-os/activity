import type { LoaderFunctionArgs } from "react-router";

/**
 * Health check endpoint for Kubernetes probes.
 * Returns 200 OK with "healthy" text.
 */
export async function loader({ request: _request }: LoaderFunctionArgs) {
  return new Response("healthy", {
    status: 200,
    headers: {
      "Content-Type": "text/plain",
    },
  });
}
