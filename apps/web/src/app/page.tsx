"use client";

import { usePathname } from "next/navigation";
import { RepoProvider } from "@/components/providers/repo-provider";
import { RepoPicker } from "@/components/repo-picker/repo-picker";
import { RepoView } from "@/components/repo-picker/repo-view";

// Single root page driving both the unscoped picker and the per-repo view.
//
// The Next.js build is `output: 'export'` (static HTML) and the Go static
// handler falls back to the root index.html for any extension-less path —
// effectively SPA mode. We read the pathname on the client and either show
// the picker (path "/") or render RepoView wrapped in RepoProvider scoped
// to the first path segment.
export default function Home() {
  const pathname = usePathname();
  const segments = (pathname || "/").split("/").filter(Boolean);
  const repoId = segments[0] ? decodeURIComponent(segments[0]) : "";

  if (!repoId) {
    return <RepoPicker />;
  }

  return (
    <RepoProvider repoId={repoId}>
      <RepoView />
    </RepoProvider>
  );
}
