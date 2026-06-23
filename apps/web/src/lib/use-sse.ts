"use client";

import { useEffect, useEffectEvent } from "react";
import type { FeedEvent, RepoRef } from "@/lib/api";

// Empty default = same-origin requests. See api.ts for rationale.
const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

type SSECallback = () => void;

/**
 * Connects to the per-repo SSE event stream and calls onUpdate whenever
 * the server signals that the repo's view should be refetched.
 * Optionally calls onEvent for server-sourced feed events.
 */
export function useSSE(
  ref: RepoRef,
  onUpdate: SSECallback,
  onEvent?: (event: FeedEvent) => void
) {
  const handleUpdate = useEffectEvent(onUpdate);
  const handleEvent = useEffectEvent((event: FeedEvent) => {
    onEvent?.(event);
  });

  const { owner, repo } = ref;
  useEffect(() => {
    const url = `${API_BASE}/api/v1/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/events`;
    const eventSource = new EventSource(url);

    eventSource.addEventListener("stacks_updated", () => {
      handleUpdate();
    });

    eventSource.addEventListener("branch_changed", () => {
      handleUpdate();
    });

    eventSource.addEventListener("refresh", () => {
      handleUpdate();
    });

    eventSource.addEventListener("branch_switched", (e) => {
      try {
        const data = JSON.parse(e.data);
        handleEvent({
          kind: "branch_switched",
          timestamp: data.timestamp || new Date().toISOString(),
          branch: data.to,
          detail: `from ${data.from}`,
        });
      } catch {
        // Ignore malformed events
      }
      handleUpdate();
    });

    eventSource.onerror = () => {
      // EventSource will auto-reconnect
    };

    return () => {
      eventSource.close();
    };
  }, [owner, repo]);
}
