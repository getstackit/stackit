import { AppShell } from "@/components/app-shell";

// `output: 'export'` emits a single index.html for the empty slug. Every deeper
// path (/{owner}/{repo}/...) is matched by this optional catch-all and handled
// client-side; the Go server serves the same index.html for those deep links on
// hard refresh (SPA fallback in internal/api/static.go).
export function generateStaticParams() {
  return [{ slug: [] }];
}

export default function Page() {
  return <AppShell />;
}
