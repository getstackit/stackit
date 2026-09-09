"use client";

import { useState } from "react";
import { motion, AnimatePresence } from "motion/react";
import type { BranchResponse, StackDetail } from "@/lib/api";
import { StackColumn, hasBranching } from "@/components/swimlane/stack-column";
import { StackTreeColumn } from "@/components/swimlane/stack-tree-column";
import { SwimlaneLabel, swimlaneColor, swimlaneAccent } from "@/components/swimlane/swimlane-label";

const COLLAPSED_LIMIT = 3;

interface OwnerSwimlaneProps {
  label: string;
  lastActive?: Date;
  stacks: StackDetail[];
  selectedBranch: string | null;
  selectedStack: string | null;
  onSelectBranch: (branch: BranchResponse) => void;
  onSelectStack: (stack: StackDetail) => void;
  compact?: boolean;
}

export function OwnerSwimlane({
  label,
  lastActive,
  stacks,
  selectedBranch,
  selectedStack,
  onSelectBranch,
  onSelectStack,
  compact = false,
}: OwnerSwimlaneProps) {
  const [expanded, setExpanded] = useState(false);
  const color = swimlaneColor(label);

  const canCollapse = stacks.length > COLLAPSED_LIMIT;
  const visibleStacks = canCollapse && !expanded
    ? stacks.slice(0, COLLAPSED_LIMIT)
    : stacks;
  const hiddenCount = stacks.length - COLLAPSED_LIMIT;

  const accentColor = swimlaneAccent(label);

  return (
    <div className="flex flex-col shrink-0">
      <SwimlaneLabel label={label} lastActive={lastActive} color={accentColor} compact={compact} />
      <div
        className={`flex items-end ${compact ? "gap-3 p-2" : "gap-4 p-3"} rounded-xl border`}
        style={{ backgroundColor: color }}
      >
        <AnimatePresence mode="popLayout">
          {visibleStacks.map((stack) => (
            <motion.div
              key={stack.rootBranch}
              layout
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.95 }}
              transition={{ duration: 0.2 }}
            >
              {hasBranching(stack.branches) ? (
                <StackTreeColumn
                  stack={stack}
                  selectedBranch={selectedBranch}
                  selectedStack={selectedStack}
                  onSelectBranch={onSelectBranch}
                  onSelectStack={onSelectStack}
                  compact={compact}
                />
              ) : (
                <StackColumn
                  stack={stack}
                  selectedBranch={selectedBranch}
                  selectedStack={selectedStack}
                  onSelectBranch={onSelectBranch}
                  onSelectStack={onSelectStack}
                  compact={compact}
                />
              )}
            </motion.div>
          ))}
        </AnimatePresence>
        {canCollapse && (
          <motion.button
            layout
            onClick={() => setExpanded(!expanded)}
            className={`flex items-center justify-center w-8 shrink-0 self-stretch rounded border border-dashed border-muted-foreground/30 text-muted-foreground hover:border-muted-foreground/50 hover:text-foreground transition-colors ${compact ? "text-[11px]" : "text-xs"}`}
          >
            <span className="-rotate-90 whitespace-nowrap">
              {expanded ? "Show less" : `+${hiddenCount} more`}
            </span>
          </motion.button>
        )}
      </div>
    </div>
  );
}
