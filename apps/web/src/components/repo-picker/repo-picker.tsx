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
import { AddRepository } from "./add-repository";

export function RepoPicker() {
  const router = useRouter();
  const [repos, setRepos] = useState<RepoSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const openRepo = (id: string) =>
    router.push(`/?repo=${encodeURIComponent(id)}`);

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
        <AddRepository onAdded={(repo) => openRepo(repo.id)} />
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
              onClick={() => openRepo(repo.id)}
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

      <AddRepository onAdded={(repo) => openRepo(repo.id)} />
    </div>
  );
}
