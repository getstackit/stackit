"use client";

import { useCallback, useMemo, useState } from "react";
import type { BranchResponse, StackDetail } from "@/lib/api";
import {
  buildRepoPath,
  parseRepoPath,
  type Selection,
} from "@/lib/repo-route";

export type { Selection };

function readInitialSelection(): Selection | null {
  if (typeof window === "undefined") return null;
  return parseRepoPath(window.location.pathname).selection;
}

// navigate rewrites the path for the current repo to reflect selection,
// without adding a history entry (matching the previous query-param behavior).
// The owner/repo are read back from the current path so the hook needs no repo
// props.
function navigate(selection: Selection | null) {
  const { owner, repo } = parseRepoPath(window.location.pathname);
  if (!owner || !repo) return;
  window.history.replaceState({}, "", buildRepoPath(owner, repo, selection));
}

export function useUrlSelection(stackDetails: StackDetail[]) {
  const [selection, setSelection] = useState<Selection | null>(readInitialSelection);

  const selectedBranch = useMemo(() => {
    if (!selection) return null;
    // A "pull" selection is a deep link by PR number; resolve it to the branch
    // carrying that PR once the data has loaded.
    const match =
      selection.type === "branch"
        ? (b: BranchResponse) => b.name === selection.name
        : selection.type === "pull"
          ? (b: BranchResponse) => b.pr?.number === selection.number
          : null;
    if (!match) return null;
    for (const stack of stackDetails) {
      const found = stack.branches.find(match);
      if (found) return found;
    }
    return null;
  }, [selection, stackDetails]);

  const selectedStack = useMemo(() => {
    if (!selection || selection.type !== "stack") return null;
    return stackDetails.find((s) => s.rootBranch === selection.rootBranch) ?? null;
  }, [selection, stackDetails]);

  const selectedBranchStack = useMemo(() => {
    if (!selectedBranch) return null;
    return (
      stackDetails.find((s) =>
        s.branches.some((b) => b.name === selectedBranch.name)
      ) ?? null
    );
  }, [selectedBranch, stackDetails]);

  const handleSelectBranch = useCallback((branch: BranchResponse | null) => {
    const next: Selection | null = branch ? { type: "branch", name: branch.name } : null;
    setSelection(next);
    navigate(next);
  }, []);

  const handleClearSelection = useCallback(() => {
    setSelection(null);
    navigate(null);
  }, []);

  const handleSelectStack = useCallback((stack: StackDetail) => {
    setSelection((prev) => {
      const deselecting = prev?.type === "stack" && prev.rootBranch === stack.rootBranch;
      const next = deselecting ? null : { type: "stack" as const, rootBranch: stack.rootBranch };

      queueMicrotask(() => navigate(next));

      return next;
    });
  }, []);

  const handleNavigateToBranch = useCallback(
    (name: string) => {
      for (const stack of stackDetails) {
        const found = stack.branches.find((b) => b.name === name);
        if (found) {
          handleSelectBranch(found);
          return;
        }
      }
    },
    [stackDetails, handleSelectBranch]
  );

  const handleStackBranchSelect = useCallback(
    (branch: BranchResponse) => {
      handleSelectBranch(branch);
    },
    [handleSelectBranch]
  );

  return {
    selection,
    selectedBranch,
    selectedStack,
    selectedBranchStack,
    handleSelectBranch,
    handleClearSelection,
    handleSelectStack,
    handleNavigateToBranch,
    handleStackBranchSelect,
  };
}
