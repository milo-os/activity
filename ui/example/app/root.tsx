import {
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
  type LinksFunction,
  type MetaFunction,
} from "react-router";

// Tailwind styles
import indexStyles from "./styles/index.css?url";

export const links: LinksFunction = () => [
  { rel: "stylesheet", href: indexStyles },
];

export const meta: MetaFunction = () => {
  return [
    { title: "Activity Explorer" },
    { name: "description", content: "Explore audit logs and activities" },
  ];
};

// Script to detect system theme and apply dark class before render (prevents flash)
const themeScript = `
  (function() {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    if (prefersDark) {
      document.documentElement.classList.add('dark');
    }
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
      if (e.matches) {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    });
  })();
`;

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="h-full">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        <Meta />
        <Links />
      </head>
      <body className="h-full flex flex-col overflow-hidden">
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export default function App() {
  return (
    <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
      <Outlet />
    </div>
  );
}
