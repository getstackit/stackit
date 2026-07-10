"use client";

import { useState } from "react";
import {
  GitBranch,
  Trash2,
  ArrowRightLeft,
  GitPullRequest,
  GitMerge,
  XCircle,
  CircleDot,
  AlertTriangle,
  CheckCircle2,
  Layers,
  RefreshCw,
  ChevronDown,
  ChevronRight,
  X,
} from "lucide-react";
import { useRepo } from "@/components/providers/repo-provider";
import type { EventKind } from "@/lib/api";
import { formatTimeAgo } from "@/lib/time";

const EVENT_CONFIG: Record<
  EventKind,
  { icon: typeof GitBranch; label: string; color: string }
> = {
  branch_created: {
    icon: GitBranch,
    label: "Branch created",
    color: "text-green-500",
  },
  branch_deleted: {
    icon: Trash2,
    label: "Branch deleted",
    color: "text-red-500",
  },
  branch_switched: {
    icon: ArrowRightLeft,
    label: "Switched to",
    color: "text-blue-500",
  },
  pr_opened: {
    icon: GitPullRequest,
    label: "PR opened",
    color: "text-green-500",
  },
  pr_merged: {
    icon: GitMerge,
    label: "PR merged",
    color: "text-purple-500",
  },
  pr_closed: {
    icon: XCircle,
    label: "PR closed",
    color: "text-red-500",
  },
  ci_changed: {
    icon: CircleDot,
    label: "CI status",
    color: "text-yellow-500",
  },
  needs_restack: {
    icon: AlertTriangle,
    label: "Needs restack",
    color: "text-orange-500",
  },
  restack_resolved: {
    icon: CheckCircle2,
    label: "Restack resolved",
    color: "text-green-500",
  },
  stack_created: {
    icon: Layers,
    label: "Stack created",
    color: "text-green-500",
  },
  stack_deleted: {
    icon: Layers,
    label: "Stack deleted",
    color: "text-red-500",
  },
  revision_updated: {
    icon: RefreshCw,
    label: "Updated",
    color: "text-blue-500",
  },
};

export function EventFeed() {
  const { events, clearEvents } = useRepo();
  const [collapsed, setCollapsed] = useState(false);

  return (
    <div className="flex flex-col">
      {/* Header */}
      <button
        onClick={() => setCollapsed(!collapsed)}
        className="flex items-center justify-between px-1 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <span className="flex items-center gap-1">
          {collapsed ? (
            <ChevronRight className="h-3 w-3" />
          ) : (
            <ChevronDown className="h-3 w-3" />
          )}
          Activity
          {events.length > 0 && (
            <span className="ml-1 text-[10px] bg-muted rounded-full px-1.5 py-0.5 tabular-nums">
              {events.length}
            </span>
          )}
        </span>
        {events.length > 0 && !collapsed && (
          <span
            onClick={(e) => {
              e.stopPropagation();
              clearEvents();
            }}
            className="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground cursor-pointer"
          >
            <X className="h-2.5 w-2.5" />
            Clear
          </span>
        )}
      </button>

      {/* Event list */}
      {!collapsed && (
        <div className="max-h-[300px] overflow-auto">
          {events.length === 0 ? (
            <p className="text-xs text-muted-foreground/60 py-4 text-center">
              No recent activity
            </p>
          ) : (
            <div className="flex flex-col gap-0.5">
              {events.map((event, i) => {
                const config = EVENT_CONFIG[event.kind];
                const Icon = config.icon;
                return (
                  <div
                    key={`${event.timestamp}-${event.kind}-${event.branch}-${i}`}
                    className="flex items-start gap-2 px-1 py-1 rounded-sm hover:bg-muted/50 transition-colors"
                  >
                    <Icon
                      className={`h-3.5 w-3.5 mt-0.5 shrink-0 ${config.color}`}
                    />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-baseline gap-1 text-xs">
                        <span className="text-muted-foreground">
                          {config.label}
                        </span>
                        {event.branch && (
                          <span className="font-mono text-foreground truncate">
                            {event.branch}
                          </span>
                        )}
                      </div>
                      {event.detail && (
                        <span className="text-[10px] text-muted-foreground/70">
                          {event.detail}
                        </span>
                      )}
                    </div>
                    <span className="text-[10px] text-muted-foreground/50 shrink-0 tabular-nums">
                      {formatTimeAgo(event.timestamp)}
                    </span>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
