"use client";

import {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  useRef,
  type ReactNode,
} from "react";
import {
  fetchView,
  type RepoResponse,
  type StackDetail,
  type TrunkCommitResponse,
  type ViewResponse,
  type FeedEvent,
} from "@/lib/api";
import { useSSE } from "@/lib/use-sse";
import { diffViews } from "@/lib/diff-views";

const MAX_EVENTS = 100;

interface RepoState {
  repoId: string;
  repo: RepoResponse | null;
  stackDetails: StackDetail[];
  recentlyMerged: TrunkCommitResponse[];
  loading: boolean;
  error: string | null;
  lastUpdated: Date | null;
  events: FeedEvent[];
  refresh: () => void;
  clearEvents: () => void;
}

const RepoContext = createContext<RepoState | null>(null);

export function useRepo() {
  const ctx = useContext(RepoContext);
  if (!ctx) throw new Error("useRepo must be used within RepoProvider");
  return ctx;
}

export function RepoProvider({ repoId, children }: { repoId: string; children: ReactNode }) {
  const [repo, setRepo] = useState<RepoResponse | null>(null);
  const [stackDetails, setStackDetails] = useState<StackDetail[]>([]);
  const [recentlyMerged, setRecentlyMerged] = useState<TrunkCommitResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [events, setEvents] = useState<FeedEvent[]>([]);

  const prevViewRef = useRef<ViewResponse | null>(null);

  const addEvents = useCallback((newEvents: FeedEvent[]) => {
    if (newEvents.length === 0) return;
    setEvents((prev) => [...newEvents, ...prev].slice(0, MAX_EVENTS));
  }, []);

  const addEvent = useCallback((event: FeedEvent) => {
    setEvents((prev) => [event, ...prev].slice(0, MAX_EVENTS));
  }, []);

  const clearEvents = useCallback(() => {
    setEvents([]);
  }, []);

  const loadData = useCallback(async () => {
    try {
      const view = await fetchView(repoId);
      setRepo(view.repo);

      // TODO: Remove sample stacks — for UI development only
      const sampleStacks: StackDetail[] = [
        {
          rootBranch: "sample/auth-refactor",
          title: "Refactor auth middleware",
          status: "shippable",
          branchCount: 2,
          prCount: 2,
          isCurrent: false,
          owner: "teammate-alice",
          branches: [
            {
              name: "sample/auth-refactor",
              depth: 0,
              isCurrent: false,
              needsRestack: false,
              isLocked: false,
              isFrozen: false,
              revision: "abc1234",
              commitDate: new Date().toISOString(),
              commitAuthor: "teammate-alice",
              commitCount: 3,
              linesAdded: 120,
              linesDeleted: 45,
              pr: {
                number: 101,
                title: "Refactor auth middleware",
                state: "OPEN",
                url: "#",
                isDraft: false,
                base: "main",
              },
            },
            {
              name: "sample/auth-tests",
              parent: "sample/auth-refactor",
              depth: 1,
              isCurrent: false,
              needsRestack: false,
              isLocked: false,
              isFrozen: false,
              revision: "def5678",
              commitDate: new Date().toISOString(),
              commitAuthor: "teammate-alice",
              commitCount: 1,
              linesAdded: 80,
              linesDeleted: 0,
              pr: {
                number: 102,
                title: "Add auth middleware tests",
                state: "OPEN",
                url: "#",
                isDraft: false,
                base: "sample/auth-refactor",
              },
            },
          ],
        },
        {
          rootBranch: "sample/fix-pagination",
          title: "Fix pagination off-by-one",
          status: "pending",
          branchCount: 1,
          prCount: 1,
          isCurrent: false,
          owner: "teammate-bob",
          branches: [
            {
              name: "sample/fix-pagination",
              depth: 0,
              isCurrent: false,
              needsRestack: true,
              isLocked: false,
              isFrozen: false,
              revision: "fed9876",
              commitDate: new Date().toISOString(),
              commitAuthor: "teammate-bob",
              commitCount: 1,
              linesAdded: 5,
              linesDeleted: 3,
              pr: {
                number: 200,
                title: "Fix pagination off-by-one",
                state: "OPEN",
                url: "#",
                isDraft: true,
                base: "main",
              },
            },
          ],
        },
      ];

      const augmentedView: ViewResponse = {
        repo: view.repo,
        stacks: [...view.stacks, ...sampleStacks],
      };

      // Diff against previous view to detect changes
      if (prevViewRef.current) {
        const detected = diffViews(prevViewRef.current, augmentedView);
        addEvents(detected);
      }
      prevViewRef.current = augmentedView;

      setStackDetails(augmentedView.stacks);
      setRecentlyMerged(view.recentlyMerged ?? []);
      setError(null);
      setLastUpdated(new Date());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load data");
    } finally {
      setLoading(false);
    }
  }, [addEvents, repoId]);

  // Initial load
  useEffect(() => {
    const timeout = window.setTimeout(() => {
      loadData();
    }, 0);

    return () => window.clearTimeout(timeout);
  }, [loadData]);

  // SSE updates trigger refresh; server events get added directly
  useSSE(repoId, loadData, addEvent);

  return (
    <RepoContext.Provider
      value={{
        repoId,
        repo,
        stackDetails,
        recentlyMerged,
        loading,
        error,
        lastUpdated,
        events,
        refresh: loadData,
        clearEvents,
      }}
    >
      {children}
    </RepoContext.Provider>
  );
}
