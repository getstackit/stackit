"use client";

import { useState } from "react";
import type { BranchResponse, StackDetail } from "@/lib/api";
import { BranchCard } from "./branch-card";
import { FolderGit2, ChevronDown } from "lucide-react";
import Markdown from "react-markdown";
import { stackStatusStyles } from "@/lib/status-config";

export { shortenBranchName } from "@/lib/branch-utils";

interface StackColumnProps {
  stack: StackDetail;
  selectedBranch: string | null;
  selectedStack: string | null;
  onSelectBranch: (branch: BranchResponse) => void;
  onSelectStack: (stack: StackDetail) => void;
  compact?: boolean;
  showHeader?: boolean;
  showFooter?: boolean;
  fillWidth?: boolean;
}

export function StackColumn({
  stack,
  selectedBranch,
  selectedStack,
  onSelectBranch,
  onSelectStack,
  compact = false,
  showHeader = true,
  showFooter = true,
  fillWidth = false,
}: StackColumnProps) {
  // Order branches root→leaf, then reverse so leaf is at top (stacking metaphor)
  const orderedBranches = orderBranches(stack.branches);
  const displayBranches = [...orderedBranches].reverse();

  return (
    <div className={`flex flex-col ${fillWidth ? "w-full" : "w-64 shrink-0"}`}>
      {showHeader && (
        <StackHeader stack={stack} compact={compact} />
      )}

      {/* Stacked branch cards */}
      <div className="flex flex-col rounded-t-lg overflow-hidden border-x border-t shadow-sm">
        {displayBranches.map((branch, i) => {
          const isLast = i === displayBranches.length - 1;
          // Subtle depth gradient: top card (leaf) is brightest, bottom card (root) is slightly dimmer
          const depth = displayBranches.length > 1
            ? i / (displayBranches.length - 1)
            : 0;
          const opacity = 1 - depth * 0.06;

          return (
            <BranchCard
              key={branch.name}
              branch={branch}
              isSelected={selectedBranch === branch.name}
              onClick={onSelectBranch}
              compact={compact}
              className={!isLast ? "border-b border-border/50" : ""}
              style={{ opacity }}
            />
          );
        })}
      </div>

      {/* Status footer */}
      {showFooter && (
        <StackStatusFooter
          status={stack.status}
          selected={selectedStack === stack.rootBranch}
          compact={compact}
          onClick={() => onSelectStack(stack)}
        />
      )}
    </div>
  );
}

export function StackStatusFooter({
  status,
  selected,
  onClick,
  compact = false,
}: {
  status: string;
  selected: boolean;
  onClick: () => void;
  compact?: boolean;
}) {
  const c = stackStatusStyles[status] || stackStatusStyles.incomplete;

  return (
    <button
      onClick={onClick}
      className={`flex items-center justify-center px-3 rounded-b-lg border-x border-b font-medium cursor-pointer transition-all duration-200 ${compact ? "py-1 text-[11px]" : "py-1.5 text-xs"} ${c.text} ${selected ? `${c.selectedBg} font-semibold` : `${c.bg} ${c.shadow} hover:brightness-95 dark:hover:brightness-110`}`}
    >
      {c.label}
    </button>
  );
}

/** Walk the branch tree root→leaf in depth-first order. */
function orderBranches(branches: BranchResponse[]): BranchResponse[] {
  const byName = new Map(branches.map((b) => [b.name, b]));
  const roots = branches.filter((b) => !b.parent || !byName.has(b.parent));
  const childrenOf = new Map<string, BranchResponse[]>();
  for (const branch of branches) {
    if (branch.parent) {
      const children = childrenOf.get(branch.parent) ?? [];
      children.push(branch);
      childrenOf.set(branch.parent, children);
    }
  }
  const ordered: BranchResponse[] = [];

  function walk(name: string) {
    const branch = byName.get(name);
    if (!branch) return;
    ordered.push(branch);
    const children = childrenOf.get(name) ?? [];
    for (const child of children) {
      walk(child.name);
    }
  }

  for (const root of roots) {
    walk(root.name);
  }

  return ordered;
}

export function StackHeader({
  stack,
  compact = false,
}: {
  stack: StackDetail;
  compact?: boolean;
}) {
  const hasContent = stack.hasWorktree || stack.title;
  if (!hasContent) return null;

  return (
    <div className={`px-1 ${compact ? "pb-1" : "pb-2"}`}>
      {stack.hasWorktree && (
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground flex items-center gap-1" title="Worktree">
            <FolderGit2 className="w-3 h-3" />
          </span>
        </div>
      )}
      {stack.title && (
        <p className={`font-medium text-muted-foreground truncate ${compact ? "text-[11px] mt-0.5" : "text-xs mt-1"}`} title={stack.title}>
          {stack.title}
        </p>
      )}
    </div>
  );
}

const DESCRIPTION_COLLAPSE_LENGTH = 80;

export function StackDescription({
  text,
  compact = false,
}: {
  text: string;
  compact?: boolean;
}) {
  const canCollapse = text.length > DESCRIPTION_COLLAPSE_LENGTH;
  const [collapsed, setCollapsed] = useState(canCollapse);

  return (
    <div
      onClick={canCollapse ? () => setCollapsed(!collapsed) : undefined}
      className={`${compact ? "text-[11px] mt-0.5" : "text-xs mt-1"} text-muted-foreground flex items-start gap-1 ${canCollapse ? "cursor-pointer hover:text-foreground/70" : ""}`}
    >
      <div className={`prose prose-xs dark:prose-invert max-w-none [&>*]:text-muted-foreground [&>*:first-child]:mt-0 [&>*:last-child]:mb-0 [&_ul]:my-0.5 [&_ol]:my-0.5 [&_li]:my-0 [&_p]:my-0.5 ${compact ? "[&>*]:text-[11px]" : "[&>*]:text-xs"} ${collapsed ? "line-clamp-2" : ""}`}>
        <Markdown>{text}</Markdown>
      </div>
      {canCollapse && (
        <ChevronDown
          className={`w-3 h-3 shrink-0 mt-0.5 transition-transform duration-200 ${collapsed ? "" : "rotate-180"}`}
        />
      )}
    </div>
  );
}

/** Returns true if any branch in the list has more than one child (i.e. the stack forks). */
export function hasBranching(branches: BranchResponse[]): boolean {
  return branches.some((b) => (b.children?.length ?? 0) > 1);
}
