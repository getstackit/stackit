"use client";

import { motion } from "motion/react";
import dynamic from "next/dynamic";
import {
  ArrowUp,
  ArrowDown,
  AlertTriangle,
  Lock,
  Snowflake,
  CloudOff,
  GitPullRequest,
  GitBranchPlus,
  ArrowDownToLine,
} from "lucide-react";
import type { BranchResponse } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui/tooltip";
import {
  PRBadge,
  CIStatusBadge,
  ReviewBadge,
  DiffStats,
} from "@/components/status/status-badge";
import { CommitList } from "./commit-list";
import { CIChecks } from "./ci-checks";
import { DiffSkeleton } from "./diff-skeleton";

const BranchDiff = dynamic(
  () => import("./branch-diff").then((m) => m.BranchDiff),
  {
    ssr: false,
    loading: () => <DiffSkeleton />,
  }
);

interface BranchDetailProps {
  branch: BranchResponse;
  onNavigateToBranch?: (name: string) => void;
}

export function BranchDetail({ branch, onNavigateToBranch }: BranchDetailProps) {
  return (
    <motion.div
      key={branch.name}
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.2, ease: "easeOut" }}
    >
    <Card className="overflow-hidden">
      <CardHeader className="pb-3 min-w-0">
        <div className="flex items-center justify-between gap-2 min-w-0">
          <CardTitle className="text-base truncate min-w-0" title={branch.pr ? branch.pr.title : branch.name}>
            {branch.pr ? branch.pr.title : branch.name}
          </CardTitle>
          <div className="flex items-center gap-2 shrink-0">
            <PRBadge pr={branch.pr} />
            <CIStatusBadge ci={branch.ci} />
            <ReviewBadge ci={branch.ci} />
          </div>
        </div>
        <p className="text-xs font-mono text-muted-foreground truncate min-w-0" title={branch.name}>
          {branch.name}
        </p>
      </CardHeader>

      <CardContent className="space-y-4">
        {/* Metadata row */}
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
          {branch.parent && (
            <span className="flex items-center gap-1">
              <ArrowUp className="w-3 h-3" />
              {onNavigateToBranch ? (
                <button
                  onClick={() => onNavigateToBranch(branch.parent!)}
                  className="hover:text-foreground hover:underline transition-colors"
                >
                  {branch.parent}
                </button>
              ) : (
                branch.parent
              )}
            </span>
          )}
          <span>
            {branch.commitCount} commit{branch.commitCount !== 1 && "s"}
          </span>
          <DiffStats added={branch.linesAdded} deleted={branch.linesDeleted} />
          {branch.commitAuthor && <span>{branch.commitAuthor}</span>}
        </div>

        {/* Children */}
        {branch.children && branch.children.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {branch.children.map((child) => (
              <button
                key={child}
                onClick={() => onNavigateToBranch?.(child)}
                className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-mono bg-muted hover:bg-muted/80 hover:text-foreground text-muted-foreground transition-colors"
              >
                <ArrowDown className="w-3 h-3" />
                {child}
              </button>
            ))}
          </div>
        )}

        {/* Warnings */}
        <Warnings branch={branch} />

        <hr className="border-border" />

        {/* Commits */}
        <CommitList commits={branch.commits} />

        {/* Branch diff */}
        <BranchDiff branchName={branch.name} revision={branch.revision} />

        {/* CI Checks */}
        <CIChecks ci={branch.ci} />
      </CardContent>
    </Card>
    </motion.div>
  );
}

interface WarningDef {
  label: string;
  hint: string;
  icon: React.ComponentType<{ className?: string }>;
}

function Warnings({ branch }: { branch: BranchResponse }) {
  const warnings: WarningDef[] = [];

  if (branch.needsRestack) {
    warnings.push({
      label: "Needs restack",
      hint: "A parent branch has changed. Run stackit restack to update.",
      icon: AlertTriangle,
    });
  }
  if (branch.isLocked) {
    warnings.push({
      label: `Locked${branch.lockReason ? ` (${branch.lockReason})` : ""}`,
      hint: "This branch is locked and cannot be modified until unlocked.",
      icon: Lock,
    });
  }
  if (branch.isFrozen) {
    warnings.push({
      label: "Frozen",
      hint: "This branch is frozen and won't be included in restack operations.",
      icon: Snowflake,
    });
  }
  if (branch.remoteStatus?.missingRemote) {
    warnings.push({
      label: "Not yet pushed",
      hint: "This branch hasn't been pushed to the remote. Run stackit submit to push.",
      icon: CloudOff,
    });
  }
  if (branch.remoteStatus?.behind) {
    warnings.push({
      label: "Behind remote",
      hint: "The remote has changes that aren't in your local branch. Run stackit sync.",
      icon: ArrowDownToLine,
    });
  }
  if (branch.remoteStatus?.diverged) {
    warnings.push({
      label: "Diverged from remote",
      hint: "Local and remote have different changes. Run stackit sync to reconcile.",
      icon: GitBranchPlus,
    });
  }
  if (!branch.pr) {
    warnings.push({
      label: "No PR",
      hint: "No pull request exists yet. Run stackit submit to create one.",
      icon: GitPullRequest,
    });
  }

  if (warnings.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-1">
      {warnings.map((w) => (
        <Tooltip key={w.label}>
          <TooltipTrigger asChild>
            <span className="inline-flex items-center gap-1 rounded-md bg-amber-50 dark:bg-amber-950 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300 ring-1 ring-inset ring-amber-600/20 cursor-default">
              <w.icon className="w-3 h-3" />
              {w.label}
            </span>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {w.hint}
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}
