import type { LoaderFunctionArgs, ActionFunctionArgs } from "react-router";
import https from "https";
import http from "http";
import { getKubeConfig } from "~/lib/kubeconfig.server";

/**
 * Catch-all resource route for /apis/* requests.
 * In development: Proxies requests to the Kubernetes API server using kubeconfig credentials.
 * In production: This route is not used (Gateway handles routing directly).
 */

async function proxyRequest(request: Request): Promise<Response> {
  const url = new URL(request.url);
  const path = url.pathname + url.search;

  const config = getKubeConfig();

  // Build the target URL
  const targetUrl = new URL(path, config.apiServerUrl);
  const isHttps = targetUrl.protocol === "https:";

  // Get request body if present
  let body: string | undefined;
  if (request.method !== "GET" && request.method !== "HEAD") {
    body = await request.text();
  }

  // Build headers
  const headers: Record<string, string> = {};
  request.headers.forEach((value, key) => {
    const lowerKey = key.toLowerCase();
    // Skip headers that shouldn't be proxied
    if (lowerKey !== "host" && lowerKey !== "connection" && lowerKey !== "authorization") {
      headers[key] = value;
    }
  });

  // Add bearer token authentication if available
  if (config.bearerToken) {
    headers["Authorization"] = `Bearer ${config.bearerToken}`;
  }

  return new Promise((resolve) => {
    const options: https.RequestOptions = {
      hostname: targetUrl.hostname,
      port: targetUrl.port || (isHttps ? 443 : 80),
      path: targetUrl.pathname + targetUrl.search,
      method: request.method,
      headers,
      // TLS configuration
      ...(isHttps
        ? {
            // Use CA cert for server verification if available
            ...(config.caCert ? { ca: config.caCert } : {}),
            // Add client certificates for mTLS if available
            ...(config.clientCert && config.clientKey
              ? {
                  cert: config.clientCert,
                  key: config.clientKey,
                }
              : {}),
            // Only reject unauthorized if we have a CA to verify against
            rejectUnauthorized: !!config.caCert,
          }
        : {}),
    };

    const transport = isHttps ? https : http;
    const proxyReq = transport.request(options, (proxyRes) => {
      const responseHeaders = new Headers();
      Object.entries(proxyRes.headers).forEach(([key, value]) => {
        if (
          value &&
          key.toLowerCase() !== "transfer-encoding" &&
          key.toLowerCase() !== "connection"
        ) {
          responseHeaders.set(key, Array.isArray(value) ? value.join(", ") : value);
        }
      });

      const chunks: Buffer[] = [];
      proxyRes.on("data", (chunk) => chunks.push(chunk));
      proxyRes.on("end", () => {
        const responseBody = Buffer.concat(chunks);
        resolve(
          new Response(responseBody, {
            status: proxyRes.statusCode || 500,
            statusText: proxyRes.statusMessage || "Unknown",
            headers: responseHeaders,
          })
        );
      });
    });

    proxyReq.on("error", (error) => {
      console.error("Proxy error:", error);
      resolve(
        new Response(
          JSON.stringify({
            error: "Failed to proxy request",
            message: error.message,
          }),
          {
            status: 502,
            headers: { "Content-Type": "application/json" },
          }
        )
      );
    });

    if (body) {
      proxyReq.write(body);
    }
    proxyReq.end();
  });
}

export async function loader({ request }: LoaderFunctionArgs) {
  return proxyRequest(request);
}

export async function action({ request }: ActionFunctionArgs) {
  return proxyRequest(request);
}
