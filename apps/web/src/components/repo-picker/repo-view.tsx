"use client";

import { useMemo, useState } from "react";
import dynamic from "next/dynamic";
import { useRepo } from "@/components/providers/repo-provider";
import { OwnerSwimlane } from "@/components/swimlane/owner-swimlane";
import { getLastActiveDate } from "@/lib/swimlane-grouping";
import { BranchDetail } from "@/components/branch-detail/branch-detail";
import { StackDetailPanel } from "@/components/branch-detail/stack-detail";
import { DetailEmptyState } from "@/components/branch-detail/detail-empty-state";
import { EventFeed } from "@/components/layout/event-feed";
import { Header } from "@/components/layout/header";
import { Separator } from "@/components/ui/separator";
import { RecentlyMerged } from "@/components/recently-merged/recently-merged";
import { BackgroundMesh } from "@/components/ui/background-mesh";
import { SkeletonSwimlane } from "@/components/ui/skeleton-shimmer";
import { useUrlSelection } from "@/hooks/use-url-selection";
import { groupStacksByOwner } from "@/lib/swimlane-grouping";

const BranchDiffWorkspace = dynamic(
  () =>
    import("@/components/branch-detail/branch-diff-workspace").then(
      (m) => m.BranchDiffWorkspace
    ),
  {
    ssr: false,
    loading: () => (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading diff workspace...
      </div>
    ),
  }
);

export function RepoView() {
  const {
    repo,
    stackDetails,
    loading,
    error,
    lastUpdated,
    refresh,
  } = useRepo();

  const {
    selection,
    selectedBranch,
    selectedStack,
    selectedBranchStack,
    handleSelectBranch,
    handleClearSelection,
    handleSelectStack,
    handleNavigateToBranch,
    handleStackBranchSelect,
  } = useUrlSelection(stackDetails);

  const { yourStacks, otherOwners } = useMemo(
    () => groupStacksByOwner(stackDetails, repo?.currentUser),
    [stackDetails, repo?.currentUser]
  );

  const [recentCommitsPreference, setRecentCommitsPreference] = useState(true);
  const hasSelection = selectedBranch || selectedStack;
  const showRecentCommits = selectedBranch ? false : recentCommitsPreference;
  const branchOverlayMode = Boolean(selectedBranch && selectedBranchStack);
  const stacksAndHistoryContent =
    stackDetails.length > 0 ? (
      <div className="flex flex-col justify-end min-h-full">
        {/* Swimlanes: only this area scrolls horizontally */}
        <div className="overflow-x-auto">
          <div className="flex items-end gap-6 p-6 pb-4 min-w-max">
            {/* Your stacks */}
            {yourStacks.length > 0 && (
              <OwnerSwimlane
                label="You"
                stacks={yourStacks}
                selectedBranch={selectedBranch?.name ?? null}
                selectedStack={selection?.type === "stack" ? selection.rootBranch : null}
                onSelectBranch={handleSelectBranch}
                onSelectStack={handleSelectStack}
                compact={branchOverlayMode}
              />
            )}

            {/* Teammate swimlanes */}
            {otherOwners.map(([owner, stacks]) => (
              <OwnerSwimlane
                key={owner}
                label={`@${owner}`}
                lastActive={getLastActiveDate(stacks)}
                stacks={stacks}
                selectedBranch={selectedBranch?.name ?? null}
                selectedStack={selection?.type === "stack" ? selection.rootBranch : null}
                onSelectBranch={handleSelectBranch}
                onSelectStack={handleSelectStack}
                compact={branchOverlayMode}
              />
            ))}
          </div>
        </div>

        {/* Trunk line */}
        <div className="flex items-center gap-2 px-6 pb-2 shrink-0">
          <div className="flex-1 h-[2px] bg-gradient-to-r from-transparent via-muted-foreground/30 to-muted-foreground/30" />
          {selectedBranch ? (
            <span className="text-xs font-mono text-muted-foreground/70 px-2">
              {repo?.trunk}
            </span>
          ) : (
            <button
              onClick={() => setRecentCommitsPreference((prev) => !prev)}
              className="text-xs font-mono text-muted-foreground/70 px-2 hover:text-muted-foreground transition-colors cursor-pointer"
            >
              {repo?.trunk}
            </button>
          )}
          <div className="flex-1 h-[2px] bg-gradient-to-l from-transparent via-muted-foreground/30 to-muted-foreground/30" />
        </div>

        {/* Recent trunk commits */}
        <div
          className="grid transition-[grid-template-rows,opacity] duration-300 ease-in-out"
          style={{
            gridTemplateRows: showRecentCommits ? "1fr" : "0fr",
            opacity: showRecentCommits ? 1 : 0,
          }}
        >
          <div className="overflow-hidden">
            <RecentlyMerged compact={branchOverlayMode} />
          </div>
        </div>
      </div>
    ) : (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No stacks found
      </div>
    );

  if (loading) {
    return (
      <>
        <BackgroundMesh />
        <SkeletonSwimlane />
      </>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center h-screen gap-4">
        <p className="text-destructive">{error}</p>
        <p className="text-sm text-muted-foreground">
          Make sure stackit-web is running on{" "}
          {process.env.NEXT_PUBLIC_API_URL || "the same origin as this page"}
        </p>
        <button
          onClick={refresh}
          className="text-sm underline text-muted-foreground hover:text-foreground"
        >
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-screen">
      <BackgroundMesh />
      <Header repo={repo ?? null} lastUpdated={lastUpdated ?? null} refresh={refresh} />

      {/* Main content: stacks area + detail panel */}
      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden">
          {branchOverlayMode && selectedBranch && selectedBranchStack ? (
            <>
              <div className="min-h-0 flex-1 overflow-hidden border-b">
                <BranchDiffWorkspace
                  branch={selectedBranch}
                  onExit={handleClearSelection}
                />
              </div>
              <div className="h-[30vh] min-h-[180px] max-h-[340px] overflow-y-auto overflow-x-hidden bg-background/70">
                {stacksAndHistoryContent}
              </div>
            </>
          ) : (
            <div className="flex-1 overflow-y-auto overflow-x-hidden">
              {stacksAndHistoryContent}
            </div>
          )}
        </div>

        {/* Right: detail + event feed panel (always visible) */}
        <div className="flex shrink-0">
          <Separator orientation="vertical" />
          <div className="w-[480px] shrink-0 flex flex-col overflow-hidden">
            {branchOverlayMode && selectedBranchStack ? (
              <div className="flex-1 overflow-auto p-4">
                <StackDetailPanel
                  stack={selectedBranchStack}
                  onSelectBranch={handleStackBranchSelect}
                  selectedBranchName={selectedBranch?.name ?? null}
                />
              </div>
            ) : hasSelection ? (
              <div className="flex-1 overflow-auto p-4">
                {selectedBranch && (
                  <BranchDetail
                    branch={selectedBranch}
                    onNavigateToBranch={handleNavigateToBranch}
                  />
                )}
                {selectedStack && (
                  <StackDetailPanel
                    stack={selectedStack}
                    onSelectBranch={handleStackBranchSelect}
                    selectedBranchName={null}
                  />
                )}
              </div>
            ) : (
              <div className="flex-1">
                <DetailEmptyState />
              </div>
            )}
            {!branchOverlayMode && (
              <>
                <Separator />
                <div className="p-3 overflow-auto">
                  <EventFeed />
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
