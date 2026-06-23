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

      // Diff against previous view to detect changes
      if (prevViewRef.current) {
        const detected = diffViews(prevViewRef.current, view);
        addEvents(detected);
      }
      prevViewRef.current = view;

      setStackDetails(view.stacks);
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
