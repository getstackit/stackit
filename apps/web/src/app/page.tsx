"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { AuthProvider } from "@/components/providers/auth-provider";
import { RepoProvider } from "@/components/providers/repo-provider";
import { RepoPicker } from "@/components/repo-picker/repo-picker";
import { RepoView } from "@/components/repo-picker/repo-view";

// NEXT_PUBLIC_STACKIT_AUTH_DISABLED=1 short-circuits the /auth/me check
// at build time. Useful for `next dev` against a server started with
// -auth-disabled, or for builds intended for deployments where the
// operator has fronted the server with platform auth (Tailscale,
// Cloudflare Access).
const AUTH_DISABLED = process.env.NEXT_PUBLIC_STACKIT_AUTH_DISABLED === "1";

// Single root page driving both the unscoped picker and the per-repo view.
//
// The Next.js build is `output: 'export'`, so only the root URL is emitted.
// We scope to a repo via the `?repo=<id>` query string rather than a path
// segment — that way the same static index.html handles every repo in both
// `next dev` and the embedded production server, with no per-repo route to
// pre-generate.
function Home() {
  const params = useSearchParams();
  const repoId = params.get("repo") ?? "";

  if (!repoId) {
    return <RepoPicker />;
  }

  return (
    <RepoProvider repoId={repoId}>
      <RepoView />
    </RepoProvider>
  );
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <AuthProvider disable={AUTH_DISABLED}>
        <Home />
      </AuthProvider>
    </Suspense>
  );
}
