"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { AuthProvider } from "@/components/providers/auth-provider";
import { ConfigProvider } from "@/components/providers/config-provider";
import { RepoProvider } from "@/components/providers/repo-provider";
import { RepoPicker } from "@/components/repo-picker/repo-picker";
import { RepoView } from "@/components/repo-picker/repo-view";
import { ReadOnlyBanner } from "@/components/read-only-banner";
import type { ConfigResponse } from "@/lib/api";

// The server's /api/v1/config endpoint is authoritative for read-only and
// auth-required at runtime. NEXT_PUBLIC_STACKIT_AUTH_DISABLED=1 only seeds
// the fallback used when that endpoint can't be reached (e.g. an older
// server), preserving the previous build-time behavior for `next dev`
// against a server started with -auth-disabled, or platform-authed deploys.
const AUTH_DISABLED = process.env.NEXT_PUBLIC_STACKIT_AUTH_DISABLED === "1";

// Stable reference so ConfigProvider's effect doesn't re-run each render.
const CONFIG_FALLBACK: ConfigResponse = {
  readOnly: false,
  authRequired: !AUTH_DISABLED,
};

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

  return (
    <>
      <ReadOnlyBanner />
      {repoId ? (
        <RepoProvider repoId={repoId}>
          <RepoView />
        </RepoProvider>
      ) : (
        <RepoPicker />
      )}
    </>
  );
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <ConfigProvider fallback={CONFIG_FALLBACK}>
        <AuthProvider>
          <Home />
        </AuthProvider>
      </ConfigProvider>
    </Suspense>
  );
}
