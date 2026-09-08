"use client";

import { useRef } from "react";
import { Search, X } from "lucide-react";

interface StackToolbarProps {
  query: string;
  onQueryChange: (query: string) => void;
  matchingCount: number;
  totalCount: number;
}

export function StackToolbar({ query, onQueryChange, matchingCount, totalCount }: StackToolbarProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const clearSearch = () => {
    onQueryChange("");
    inputRef.current?.focus();
  };

  return (
    <div className="shrink-0 border-b bg-background px-4 py-4 sm:px-6">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-lg font-semibold tracking-tight">Stacks</h1>
          <span role="status" className="rounded-full bg-muted px-2.5 py-1 text-xs tabular-nums text-muted-foreground">
            {query.trim() ? `${matchingCount} of ${totalCount}` : totalCount}{" "}
            {totalCount === 1 ? "stack" : "stacks"}
          </span>
        </div>
        <div role="search" aria-label="Stacks" className="w-full xl:max-w-sm">
          <label htmlFor="stack-search" className="sr-only">Search stacks</label>
          <div className="flex h-10 items-center gap-2 rounded-lg border bg-muted/30 px-3 transition-colors focus-within:border-ring focus-within:bg-background focus-within:ring-2 focus-within:ring-ring/20">
            <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            <input
              ref={inputRef}
              id="stack-search"
              type="search"
              value={query}
              onChange={(event) => onQueryChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Escape" && query) {
                  event.preventDefault();
                  clearSearch();
                }
              }}
              placeholder="Branch, PR, or @owner…"
              className="h-full min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:appearance-none"
            />
            {query && (
              <button
                type="button"
                onClick={clearSearch}
                aria-label="Clear search"
                className="-mr-1 flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <X aria-hidden="true" className="size-3.5" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
