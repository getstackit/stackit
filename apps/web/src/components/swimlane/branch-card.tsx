"use client";

import type { BranchResponse } from "@/lib/api";
import { PRBadge, DiffStats } from "@/components/status/status-badge";
import { CIStatusWithTooltip } from "@/components/status/ci-status";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { GitCommitVertical, GitPullRequestDraft, Lock } from "lucide-react";
import { shortenBranchName } from "@/lib/branch-utils";

interface BranchCardProps {
  branch: BranchResponse;
  isSelected: boolean;
  onClick: (branch: BranchResponse) => void;
  compact?: boolean;
  className?: string;
  style?: React.CSSProperties;
}

export function BranchCard({
  branch,
  isSelected,
  onClick,
  compact = false,
  className = "",
  style,
}: BranchCardProps) {
  return (
    <button
      onClick={() => onClick(branch)}
      className={`text-left bg-card transition-all duration-200
        outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring
        ${compact ? "px-2.5 py-1.5" : "px-3.5 py-3.5"}
        ${isSelected ? "!bg-accent z-10 relative ring-2 ring-inset ring-ring" : "hover:!bg-muted/80"}
        ${branch.isCurrent ? "border-l-[3px] border-l-[var(--glow-color-current)] bg-accent/30" : ""}
        ${branch.isLocked ? "opacity-60" : ""}
        ${className}
      `}
      style={{
        ...style,
        ...(branch.remoteStatus?.missingRemote ? {
          backgroundImage: "repeating-linear-gradient(-45deg, transparent, transparent 8px, oklch(0.65 0.05 250 / 0.12) 8px, oklch(0.65 0.05 250 / 0.12) 12px)",
        } : undefined),
      }}
    >
      <div className="flex items-center gap-2 min-w-0">
        <span className={`text-sm font-medium leading-snug ${compact ? "truncate" : "line-clamp-2 break-words"}`} title={branch.pr?.title || branch.commits?.at(-1)?.message || branch.name}>
          {branch.pr?.title || branch.commits?.at(-1)?.message || shortenBranchName(branch.name)}
        </span>
        {branch.isLocked && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Lock className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
            </TooltipTrigger>
            <TooltipContent side="top">
              {branch.lockReason ? `Locked: ${branch.lockReason}` : "Locked"}
            </TooltipContent>
          </Tooltip>
        )}
        {branch.needsRestack && (
          <span className="text-amber-500 shrink-0" title="Needs restack">
            &#x21BB;
          </span>
        )}
      </div>
      {!compact && (
        <div className="mt-2.5 flex flex-wrap items-center gap-x-2 gap-y-1.5">
          {branch.pr ? (
            <PRBadge pr={branch.pr} />
          ) : (
            <Tooltip>
              <TooltipTrigger asChild>
                <GitPullRequestDraft className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
              </TooltipTrigger>
              <TooltipContent side="top">No PR</TooltipContent>
            </Tooltip>
          )}
          <CIStatusWithTooltip ci={branch.ci} />
          <DiffStats added={branch.linesAdded} deleted={branch.linesDeleted} />
          {branch.commitCount > 0 && (
            <span className="flex items-center gap-0.5 text-xs text-muted-foreground" title={`${branch.commitCount} commit${branch.commitCount !== 1 ? "s" : ""}`}>
              <GitCommitVertical className="w-3 h-3" />
              {branch.commitCount}
            </span>
          )}
        </div>
      )}
    </button>
  );
}
