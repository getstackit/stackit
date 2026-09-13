"use client";

import { useMemo, useState } from "react";
import dynamic from "next/dynamic";
import { ChevronDown, GitBranch, SearchX, X } from "lucide-react";
import { useRepo } from "@/components/providers/repo-provider";
import { OwnerSwimlane } from "@/components/swimlane/owner-swimlane";
import { getLastActiveDate } from "@/lib/swimlane-grouping";
import { BranchDetail } from "@/components/branch-detail/branch-detail";
import { StackDetailPanel } from "@/components/branch-detail/stack-detail";
import { DetailEmptyState } from "@/components/branch-detail/detail-empty-state";
import { EventFeed } from "@/components/layout/event-feed";
import { Header } from "@/components/layout/header";
import { RecentlyMerged } from "@/components/recently-merged/recently-merged";
import { BackgroundMesh } from "@/components/ui/background-mesh";
import { SkeletonSwimlane } from "@/components/ui/skeleton-shimmer";
import { useUrlSelection } from "@/hooks/use-url-selection";
import { groupStacksByOwner } from "@/lib/swimlane-grouping";
import { filterRecentCommits, filterStacks } from "@/lib/stack-search";
import { RepositorySearch } from "@/components/layout/repository-search";
import { cn } from "@/lib/utils";

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
    recentlyMerged,
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

  const [searchQuery, setSearchQuery] = useState("");
  const filteredStacks = useMemo(
    () => filterStacks(stackDetails, searchQuery),
    [stackDetails, searchQuery]
  );
  const filteredCommits = useMemo(
    () => filterRecentCommits(recentlyMerged ?? [], searchQuery),
    [recentlyMerged, searchQuery]
  );
  const { yourStacks, otherOwners } = useMemo(
    () => groupStacksByOwner(filteredStacks, repo?.currentUser),
    [filteredStacks, repo?.currentUser]
  );

  const [recentCommitsPreference, setRecentCommitsPreference] = useState(true);
  const hasSelection = selectedBranch || selectedStack;
  const showRecentCommits = selectedBranch ? false : recentCommitsPreference;
  const branchOverlayMode = Boolean(selectedBranch && selectedBranchStack);
  const stacksAndHistoryContent =
    stackDetails.length > 0 || (recentlyMerged?.length ?? 0) > 0 ? (
      <div className={cn("flex min-h-full flex-col", branchOverlayMode && "justify-end")}>
        {filteredStacks.length === 0 && filteredCommits.length === 0 && (
          <div className="flex flex-1 flex-col items-center justify-center px-6 py-12 text-center">
            <div className="mb-4 rounded-2xl border bg-background p-3 text-muted-foreground shadow-sm">
              <SearchX aria-hidden="true" className="size-5" />
            </div>
            <h2 className="text-sm font-medium">No results found.</h2>
            <p className="mt-1 max-w-xs text-sm text-muted-foreground">
              Try a branch, PR, owner, or commit message.
            </p>
            <button
              type="button"
              onClick={() => setSearchQuery("")}
              className="mt-4 rounded-lg border bg-background px-3 py-2 text-sm font-medium shadow-sm hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              Clear search
            </button>
          </div>
        )}
        {/* Swimlanes: only this area scrolls horizontally */}
        <div className={cn("overflow-x-auto", filteredStacks.length === 0 && "hidden")}>
          <div className="flex min-w-max items-end gap-6 p-4 pb-6 sm:p-6 sm:pb-8">
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
              type="button"
              aria-expanded={showRecentCommits}
              aria-controls="recent-trunk-commits"
              onClick={() => setRecentCommitsPreference((prev) => !prev)}
              className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <GitBranch aria-hidden="true" className="size-3.5" />
              <span className="font-mono">{repo?.trunk}</span>
              <span>Recent commits</span>
              <ChevronDown aria-hidden="true" className={cn("size-3.5 transition-transform", !showRecentCommits && "-rotate-90")} />
            </button>
          )}
          <div className="flex-1 h-[2px] bg-gradient-to-l from-transparent via-muted-foreground/30 to-muted-foreground/30" />
        </div>

        {/* Recent trunk commits */}
        <div
          id="recent-trunk-commits"
          inert={!showRecentCommits}
          className="grid transition-[grid-template-rows,opacity] duration-300 ease-in-out"
          style={{
            gridTemplateRows: showRecentCommits ? "1fr" : "0fr",
            opacity: showRecentCommits ? 1 : 0,
          }}
        >
          <div className="overflow-hidden">
            <RecentlyMerged compact={branchOverlayMode} commits={filteredCommits} />
          </div>
        </div>
      </div>
    ) : (
      <div className="flex h-full flex-col items-center justify-center px-6 py-12 text-center">
        <div className="mb-4 rounded-2xl border bg-background p-3 text-muted-foreground shadow-sm">
          <GitBranch aria-hidden="true" className="size-5" />
        </div>
        <h2 className="text-base font-medium">No stacks yet</h2>
        <p className="mt-2 max-w-xs text-sm leading-relaxed text-muted-foreground">
          Tracked branches will appear here, grouped into stacks by owner.
        </p>
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
    <div className="flex h-dvh flex-col bg-muted/40">
      <h1 className="sr-only">{repo?.repo} repository</h1>
      <Header
        repo={repo ?? null}
        lastUpdated={lastUpdated ?? null}
        refresh={refresh}
        search={
          <RepositorySearch
            query={searchQuery}
            onQueryChange={(query) => {
              setSearchQuery(query);
              if (query.trim()) setRecentCommitsPreference(true);
            }}
            stackCount={filteredStacks.length}
            commitCount={filteredCommits.length}
          />
        }
      />

      {/* Main content: stacks area + detail panel.
          Stacks vertically on mobile (detail panel below) and sits side-by-side
          on desktop. Without the column fallback the fixed-width detail panel
          (shrink-0) squeezes the flex-1 stacks area to ~0 on narrow viewports,
          so no data renders. */}
      <div className="flex flex-1 flex-col overflow-hidden md:flex-row">
        <div className="flex flex-1 flex-col overflow-hidden min-h-0 min-w-0">
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

        {/* Detail + event feed panel (always visible): below the stacks on
            mobile (bounded height), beside them on desktop (fixed width). */}
        <div className="flex shrink-0 flex-col border-t md:flex-row md:border-t-0">
          <div aria-hidden className="hidden w-px shrink-0 bg-border md:block" />
          <aside aria-label="Stack details and activity" className={cn(
            "flex w-full shrink-0 flex-col overflow-hidden bg-background md:h-auto md:w-[320px] 2xl:w-[360px]",
            hasSelection ? "h-[45dvh]" : "max-h-[25dvh] md:max-h-none"
          )}>
            <div className={cn("min-h-14 shrink-0 items-center justify-between border-b px-5 py-3", hasSelection ? "flex" : "hidden md:flex")}>
              <h2 className="text-sm font-medium">{hasSelection ? "Stack details" : "Overview"}</h2>
              {hasSelection && (
                <button
                  type="button"
                  onClick={handleClearSelection}
                  aria-label="Close details"
                  className="flex size-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <X aria-hidden="true" className="size-4" />
                </button>
              )}
            </div>
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
              <div className="hidden min-h-0 flex-1 overflow-y-auto md:block">
                <DetailEmptyState stacks={stackDetails} />
              </div>
            )}
            {!branchOverlayMode && (
              <>
                <hr className="border-border" />
                <div className="p-3 overflow-auto">
                  <EventFeed />
                </div>
              </>
            )}
          </aside>
        </div>
      </div>
    </div>
  );
}
