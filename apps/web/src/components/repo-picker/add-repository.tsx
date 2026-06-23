"use client";

import { useState, type FormEvent } from "react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useConfig } from "@/components/providers/config-provider";
import { onboardRepo, type RepoSummary } from "@/lib/api";

interface AddRepositoryProps {
  /** Called with the newly served repo after a successful onboard. */
  onAdded: (repo: RepoSummary) => void;
}

const inputClasses =
  "flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";

export function AddRepository({ onAdded }: AddRepositoryProps) {
  const { readOnly } = useConfig();
  const [owner, setOwner] = useState("");
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A read-only server refuses writes; hide the affordance entirely.
  if (readOnly) return null;

  const canSubmit = owner.trim() !== "" && name.trim() !== "" && !submitting;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const repo = await onboardRepo(owner.trim(), name.trim());
      setOwner("");
      setName("");
      onAdded(repo);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add repository");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Add a repository</CardTitle>
        <CardDescription>
          Clone a GitHub repository you have access to and start managing its
          stacks.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-3"
          aria-label="Add repository"
        >
          <div className="flex flex-col gap-3 sm:flex-row">
            <input
              type="text"
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              placeholder="owner"
              aria-label="Repository owner"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              disabled={submitting}
              className={inputClasses}
            />
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="repository"
              aria-label="Repository name"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              disabled={submitting}
              className={inputClasses}
            />
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow-sm transition-colors hover:bg-primary/90 disabled:opacity-50"
            >
              {submitting ? "Adding…" : "Add"}
            </button>
          </div>
          {error && (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          )}
        </form>
      </CardContent>
    </Card>
  );
}
