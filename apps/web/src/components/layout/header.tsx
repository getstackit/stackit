import Link from "next/link";
import type { ReactNode } from "react";
import { Layers, RefreshCw } from "lucide-react";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import { UserMenu } from "@/components/layout/user-menu";
import { formatTimeAgo } from "@/lib/time";
import type { RepoResponse } from "@/lib/api";

interface HeaderProps {
  // All props are optional so the header can render on the repo picker page,
  // which has no active repo and no refreshable view. The repo name, the
  // "updated" timestamp, and the refresh button each appear only when their
  // data is provided.
  repo?: RepoResponse | null;
  lastUpdated?: Date | null;
  refresh?: () => void;
  search?: ReactNode;
}

export function Header({ repo, lastUpdated, refresh, search }: HeaderProps) {
  return (
    <header className={`relative z-10 grid min-h-14 shrink-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 border-b bg-background px-4 sm:px-6 ${search ? "lg:grid-cols-[minmax(0,1fr)_minmax(16rem,28rem)_minmax(0,1fr)]" : ""}`}>
      <div className="flex min-h-14 min-w-0 items-center gap-3">
        <Link href="/" className="flex shrink-0 items-center gap-2 rounded-md text-sm font-semibold tracking-tight transition-colors hover:text-foreground/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <span className="flex size-7 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Layers aria-hidden="true" className="size-4" />
          </span>
          stackit
        </Link>
        {repo && (
          <>
            <span aria-hidden="true" className="text-border">/</span>
            <span className="truncate text-sm text-muted-foreground" title={`${repo.owner}/${repo.repo}`}>
              <span className="hidden lg:inline">{repo.owner}/</span><span className="font-medium text-foreground">{repo.repo}</span>
            </span>
          </>
        )}
      </div>
      {search && <div className="order-last col-span-2 min-w-0 pb-3 lg:order-none lg:col-span-1 lg:py-2">{search}</div>}
      <div className="flex min-h-14 shrink-0 items-center justify-self-end gap-1.5 sm:gap-3">
        {lastUpdated && (
          <span className="hidden text-xs text-muted-foreground xl:inline">
            Updated {formatTimeAgo(lastUpdated)}
          </span>
        )}
        <ThemeToggle />
        {refresh && (
          <button
            type="button"
            onClick={refresh}
            aria-label="Refresh repository"
            className="flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            title="Refresh"
          >
            <RefreshCw aria-hidden="true" className="size-3.5" />
          </button>
        )}
        <UserMenu />
      </div>
    </header>
  );
}
