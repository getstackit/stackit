"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { RepoProvider } from "@/components/providers/repo-provider";
import { RepoPicker } from "@/components/repo-picker/repo-picker";
import { RepoView } from "@/components/repo-picker/repo-view";

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
      <Home />
    </Suspense>
  );
}
