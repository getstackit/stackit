"use client";

import { usePathname } from "next/navigation";
import { AuthProvider } from "@/components/providers/auth-provider";
import { ConfigProvider, useConfig } from "@/components/providers/config-provider";
import { RepoProvider } from "@/components/providers/repo-provider";
import { RepoPicker } from "@/components/repo-picker/repo-picker";
import { RepoView } from "@/components/repo-picker/repo-view";
import { ReadOnlyBanner } from "@/components/read-only-banner";
import { parseRepoPath } from "@/lib/repo-route";
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
  singleRepo: false,
};

// Client shell driving both the unscoped picker and the per-repo view.
//
// The Next.js build is `output: 'export'`, so only index.html is emitted; the
// optional catch-all route ([[...slug]]) lets that one shell handle every path.
// We scope to a repo via the path — GitHub-style `/{owner}/{repo}/...` — parsed
// here from usePathname, with the Go server's SPA fallback serving the same
// shell for deep links on refresh (see internal/api/static.go).
function Home() {
  const { owner, repo } = parseRepoPath(usePathname());
  const { singleRepo } = useConfig();

  return (
    <>
      <ReadOnlyBanner />
      {owner && repo ? (
        <RepoProvider owner={owner} repo={repo}>
          <RepoView />
        </RepoProvider>
      ) : (
        <RepoPicker autoOpenSingle={singleRepo} />
      )}
    </>
  );
}

export function AppShell() {
  return (
    <ConfigProvider fallback={CONFIG_FALLBACK}>
      <AuthProvider>
        <Home />
      </AuthProvider>
    </ConfigProvider>
  );
}
