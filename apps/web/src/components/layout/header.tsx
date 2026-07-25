import Link from "next/link";
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
}

export function Header({ repo, lastUpdated, refresh }: HeaderProps) {
  return (
    <header className="relative flex items-center justify-between px-4 py-2 border-b shrink-0">
      <div className="flex items-center gap-3">
        <Link href="/" className="font-semibold text-sm hover:text-foreground/80 transition-colors">
          stackit
        </Link>
        {repo && (
          <span className="text-sm text-muted-foreground font-mono">
            {repo.owner}/{repo.repo}
          </span>
        )}
      </div>
      <div className="flex items-center gap-3">
        {lastUpdated && (
          <span className="text-xs text-muted-foreground">
            {formatTimeAgo(lastUpdated)}
          </span>
        )}
        <ThemeToggle />
        {refresh && (
          <button
            onClick={refresh}
            className="text-xs text-muted-foreground hover:text-foreground"
            title="Refresh"
          >
            &#x21BB;
          </button>
        )}
        <UserMenu />
      </div>
      {/* Animated gradient accent bar */}
      <div
        className="absolute bottom-0 left-0 right-0 h-0.5 animate-gradient-shift"
        style={{
          background: "linear-gradient(90deg, var(--gradient-start), var(--gradient-mid), var(--gradient-end), var(--gradient-start))",
        }}
      />
    </header>
  );
}
