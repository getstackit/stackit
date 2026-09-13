"use client";

import { useMemo } from "react";
import { useRepo } from "@/components/providers/repo-provider";
import { groupByTime } from "@/lib/time";
import { CommitItem } from "./commit-items";
import type { TrunkCommitResponse } from "@/lib/api";

export function RecentlyMerged({ compact = false, commits }: { compact?: boolean; commits: TrunkCommitResponse[] }) {
  const { repo } = useRepo();

  const groups = useMemo(
    () => groupByTime(commits),
    [commits]
  );

  if (groups.length === 0) {
    return null;
  }

  return (
    <div
      className={`${compact ? "px-4 pb-2 max-h-36 space-y-1.5" : "px-6 pb-4 max-h-64 space-y-2"} overflow-y-auto`}
    >
      {groups.map((group) => (
        <div key={group.label}>
          <div className={`${compact ? "text-[10px]" : "text-[11px]"} font-medium text-muted-foreground uppercase tracking-wider mb-2`}>
            {group.label}
          </div>
          <div>
            {group.items.map((commit) => (
              <CommitItem
                key={commit.sha}
                commit={commit}
                owner={repo?.owner}
                repoName={repo?.repo}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
