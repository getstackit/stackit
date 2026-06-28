"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { fetchRepos, type RepoSummary } from "@/lib/api";
import { buildRepoPath } from "@/lib/repo-route";
import { AddRepository } from "./add-repository";

interface RepoPickerProps {
  // autoOpenSingle navigates straight to the sole repo (single-repo / local
  // mode) instead of rendering the picker — choosing a repo is only meaningful
  // in the hosted, multi-repo model.
  autoOpenSingle?: boolean;
}

// openableRepo reports whether a repo can be addressed in the path-based UI —
// it needs GitHub coordinates, which a repo with no remote lacks.
const openableRepo = (repo: RepoSummary): repo is RepoSummary & { owner: string; repo: string } =>
  Boolean(repo.owner && repo.repo);

export function RepoPicker({ autoOpenSingle = false }: RepoPickerProps) {
  const router = useRouter();
  const [repos, setRepos] = useState<RepoSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Repos are addressed by their GitHub coordinates. A repo with no remote has
  // none and can't be opened in the path-based UI.
  const openRepo = (repo: RepoSummary) => {
    if (!openableRepo(repo)) return;
    router.push(buildRepoPath(repo.owner, repo.repo, null));
  };

  useEffect(() => {
    let active = true;
    fetchRepos()
      .then((resp) => {
        if (!active) return;
        setRepos(resp.repos);
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : "Failed to load repos");
      });
    return () => {
      active = false;
    };
  }, []);

  // In single-repo mode, redirect straight to the one repo once it loads.
  // router.replace (not push) keeps the picker out of history so Back doesn't
  // bounce here. Falls through to the normal render when the repo can't be
  // addressed (no remote) so the user still sees a useful state.
  const soleRepo = autoOpenSingle && repos?.length === 1 ? repos[0] : null;
  useEffect(() => {
    if (soleRepo && openableRepo(soleRepo)) {
      router.replace(buildRepoPath(soleRepo.owner, soleRepo.repo, null));
    }
  }, [soleRepo, router]);

  if (soleRepo && openableRepo(soleRepo)) {
    return (
      <div className="flex h-screen items-center justify-center text-sm text-muted-foreground">
        Opening {soleRepo.displayName || soleRepo.id}…
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-2 text-sm">
        <p className="text-destructive">{error}</p>
        <p className="text-muted-foreground">
          Make sure stackit-server is running on{" "}
          {process.env.NEXT_PUBLIC_API_URL || "the same origin as this page"}
        </p>
      </div>
    );
  }

  if (repos === null) {
    return (
      <div className="flex h-screen items-center justify-center text-sm text-muted-foreground">
        Loading repositories…
      </div>
    );
  }

  if (repos.length === 0) {
    return (
      <div className="mx-auto flex min-h-screen max-w-3xl flex-col justify-center gap-6 px-6 py-12">
        <header className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold">No repositories configured</h1>
          <p className="text-sm text-muted-foreground">
            Add a GitHub repository to get started.
          </p>
        </header>
        <AddRepository onAdded={(repo) => openRepo(repo)} />
      </div>
    );
  }

  return (
    <div className="mx-auto flex min-h-screen max-w-3xl flex-col gap-6 px-6 py-12">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold">Choose a repository</h1>
        <p className="text-sm text-muted-foreground">
          Stackit is serving {repos.length}{" "}
          {repos.length === 1 ? "repository" : "repositories"}.
        </p>
      </header>

      <ul className="flex flex-col gap-3" data-testid="repo-list">
        {repos.map((repo) => (
          <li key={repo.id}>
            <button
              type="button"
              data-testid={`repo-card-${repo.id}`}
              onClick={() => openRepo(repo)}
              className="block w-full text-left transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-xl"
            >
              <Card>
                <CardHeader>
                  <CardTitle>{repo.displayName || repo.id}</CardTitle>
                  <CardDescription>
                    <span className="font-mono">{repo.id}</span>
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex gap-4 text-xs text-muted-foreground">
                  <span>
                    trunk: <span className="font-mono">{repo.trunk}</span>
                  </span>
                  {repo.currentBranch && (
                    <span>
                      current: <span className="font-mono">{repo.currentBranch}</span>
                    </span>
                  )}
                </CardContent>
              </Card>
            </button>
          </li>
        ))}
      </ul>

      <AddRepository onAdded={(repo) => openRepo(repo)} />
    </div>
  );
}
