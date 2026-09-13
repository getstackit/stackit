"use client";

import { useRef } from "react";
import { Search, X } from "lucide-react";

interface RepositorySearchProps {
  query: string;
  onQueryChange: (query: string) => void;
  stackCount: number;
  commitCount: number;
}

export function RepositorySearch({ query, onQueryChange, stackCount, commitCount }: RepositorySearchProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const clearSearch = () => {
    onQueryChange("");
    inputRef.current?.focus();
  };

  return (
    <div role="search" aria-label="Repository">
      <label htmlFor="repository-search" className="sr-only">Search anything in this repository</label>
      <div className="flex h-9 items-center gap-2 rounded-lg border bg-muted/30 px-3 transition-colors focus-within:border-ring focus-within:bg-background focus-within:ring-2 focus-within:ring-ring/20">
        <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <input
          ref={inputRef}
          id="repository-search"
          type="search"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape" && query) {
              event.preventDefault();
              clearSearch();
            }
          }}
          placeholder="Search anything…"
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
      <span role="status" className="sr-only">
        {query.trim() ? `${stackCount} matching stacks and ${commitCount} matching recent commits` : ""}
      </span>
    </div>
  );
}
