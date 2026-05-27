"use client";

import {
  parsePatchFiles,
  type FileDiffMetadata,
} from "@pierre/diffs";
import { FileDiff } from "@pierre/diffs/react";
import { FileTree, useFileTree } from "@pierre/trees/react";
import { ChevronDown, ChevronRight, GitCommitHorizontal, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchBranchDiff, type BranchDiffResponse, type CommitResponse } from "@/lib/api";
import { useRepo } from "@/components/providers/repo-provider";
import { useTheme } from "@/components/providers/theme-provider";
import { commitUrl } from "@/lib/github";
import { cn } from "@/lib/utils";

type TreeGitStatus = "added" | "deleted" | "modified" | "renamed";

function mapDiffTypeToGitStatus(type: FileDiffMetadata["type"]): TreeGitStatus {
  switch (type) {
    case "new":
      return "added";
    case "deleted":
      return "deleted";
    case "rename-pure":
    case "rename-changed":
      return "renamed";
    default:
      return "modified";
  }
}

// Mirrors @pierre/trees' default sort: directories-first with natural-sort on segments.
function splitIntoNaturalTokens(value: string): (string | number)[] {
  const tokens: (string | number)[] = [];
  let tokenStart = 0;
  let index = 0;
  const isDigit = (code: number) => code >= 48 && code <= 57;
  while (index < value.length) {
    while (index < value.length && !isDigit(value.charCodeAt(index))) index += 1;
    if (index >= value.length) break;
    if (index > tokenStart) tokens.push(value.slice(tokenStart, index));
    let numberValue = 0;
    while (index < value.length && isDigit(value.charCodeAt(index))) {
      numberValue = numberValue * 10 + (value.charCodeAt(index) - 48);
      index += 1;
    }
    tokens.push(numberValue);
    tokenStart = index;
  }
  if (tokenStart < value.length || tokens.length === 0) {
    tokens.push(value.slice(tokenStart));
  }
  return tokens;
}

function compareSegments(left: string, right: string): number {
  const l = left.toLowerCase();
  const r = right.toLowerCase();
  const lt = splitIntoNaturalTokens(l);
  const rt = splitIntoNaturalTokens(r);
  const n = Math.min(lt.length, rt.length);
  for (let i = 0; i < n; i++) {
    if (lt[i] === rt[i]) continue;
    if (typeof lt[i] === "number" && typeof rt[i] === "number") {
      return (lt[i] as number) < (rt[i] as number) ? -1 : 1;
    }
    const ls = String(lt[i]);
    const rs = String(rt[i]);
    if (ls !== rs) return ls < rs ? -1 : 1;
  }
  if (lt.length !== rt.length) return lt.length < rt.length ? -1 : 1;
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

function compareTreePaths(a: string, b: string): number {
  const as = a.split("/");
  const bs = b.split("/");
  const shared = Math.min(as.length, bs.length);
  for (let d = 0; d < shared; d++) {
    if (as[d] === bs[d]) continue;
    const aIsDir = d < as.length - 1;
    const bIsDir = d < bs.length - 1;
    if (aIsDir !== bIsDir) return aIsDir ? -1 : 1;
    return compareSegments(as[d], bs[d]);
  }
  if (as.length !== bs.length) return as.length < bs.length ? -1 : 1;
  return 0;
}

interface BranchDiffProps {
  branchName: string;
  revision: string;
  commits?: CommitResponse[];
  onExit?: () => void;
}

const BASE_DIFF_OPTIONS = {
  diffStyle: "split",
  lineDiffType: "word",
  hunkSeparators: "metadata",
  overflow: "scroll",
} as const;

function getFileKey(file: FileDiffMetadata, index: number): string {
  return `${file.prevName ?? file.name}:${file.name}:${index}`;
}

function getFileLabel(file: FileDiffMetadata): string {
  if (file.prevName && file.prevName !== file.name) {
    return `${file.prevName} -> ${file.name}`;
  }
  return file.name;
}

function hasLineChanges(file: FileDiffMetadata): boolean {
  return file.hunks.some((hunk) =>
    hunk.hunkContent.some(
      (content) =>
        content.type === "change" &&
        (content.additions > 0 || content.deletions > 0)
    )
  );
}

function getChangeTypeIcon(file: FileDiffMetadata) {
  const colorClass =
    file.type === "new"
      ? "text-green-600"
      : file.type === "deleted"
        ? "text-red-600"
        : "text-blue-600";

  const iconType =
    file.type === "rename-pure" || file.type === "rename-changed"
      ? "rename-changed"
      : file.type;

  return (
    <svg
      className={`h-3.5 w-3.5 shrink-0 ${colorClass}`}
      data-change-icon={file.type}
      aria-hidden="true"
      viewBox="0 0 16 16"
    >
      {iconType === "new" && (
        <>
          <path d="M8 4a.75.75 0 0 1 .75.75v2.5h2.5a.75.75 0 0 1 0 1.5h-2.5v2.5a.75.75 0 0 1-1.5 0v-2.5h-2.5a.75.75 0 0 1 0-1.5h2.5v-2.5A.75.75 0 0 1 8 4" />
          <path d="M1.788 4.296c.196-.88.478-1.381.802-1.706s.826-.606 1.706-.802C5.194 1.588 6.387 1.5 8 1.5s2.806.088 3.704.288c.88.196 1.381.478 1.706.802s.607.826.802 1.706c.2.898.288 2.091.288 3.704s-.088 2.806-.288 3.704c-.195.88-.478 1.381-.802 1.706s-.826.607-1.706.802c-.898.2-2.091.288-3.704.288s-2.806-.088-3.704-.288c-.88-.195-1.381-.478-1.706-.802s-.606-.826-.802-1.706C1.588 10.806 1.5 9.613 1.5 8s.088-2.806.288-3.704M8 0C1.412 0 0 1.412 0 8s1.412 8 8 8 8-1.412 8-8-1.412-8-8-8" />
        </>
      )}
      {iconType === "deleted" && (
        <>
          <path d="M4 8a.75.75 0 0 1 .75-.75h6.5a.75.75 0 0 1 0 1.5h-6.5A.75.75 0 0 1 4 8" />
          <path d="M1.788 4.296c.196-.88.478-1.381.802-1.706s.826-.606 1.706-.802C5.194 1.588 6.387 1.5 8 1.5s2.806.088 3.704.288c.88.196 1.381.478 1.706.802s.607.826.802 1.706c.2.898.288 2.091.288 3.704s-.088 2.806-.288 3.704c-.195.88-.478 1.381-.802 1.706s-.826.607-1.706.802c-.898.2-2.091.288-3.704.288s-2.806-.088-3.704-.288c-.88-.195-1.381-.478-1.706-.802s-.606-.826-.802-1.706C1.588 10.806 1.5 9.613 1.5 8s.088-2.806.288-3.704M8 0C1.412 0 0 1.412 0 8s1.412 8 8 8 8-1.412 8-8-1.412-8-8-8" />
        </>
      )}
      {iconType === "rename-changed" && (
        <>
          <path d="M1.788 4.296c.196-.88.478-1.381.802-1.706s.826-.606 1.706-.802C5.194 1.588 6.387 1.5 8 1.5s2.806.088 3.704.288c.88.196 1.381.478 1.706.802s.607.826.802 1.706c.2.898.288 2.091.288 3.704s-.088 2.806-.288 3.704c-.195.88-.478 1.381-.802 1.706s-.826.607-1.706.802c-.898.2-2.091.288-3.704.288s-2.806-.088-3.704-.288c-.88-.195-1.381-.478-1.706-.802s-.606-.826-.802-1.706C1.588 10.806 1.5 9.613 1.5 8s.088-2.806.288-3.704M8 0C1.412 0 0 1.412 0 8s1.412 8 8 8 8-1.412 8-8-1.412-8-8-8" />
          <path d="M8.495 4.695a.75.75 0 0 0-.05 1.06L10.486 8l-2.041 2.246a.75.75 0 0 0 1.11 1.008l2.5-2.75a.75.75 0 0 0 0-1.008l-2.5-2.75a.75.75 0 0 0-1.06-.051m-4 0a.75.75 0 0 0-.05 1.06l2.044 2.248-1.796 1.995a.75.75 0 0 0 1.114 1.004l2.25-2.5a.75.75 0 0 0-.002-1.007l-2.5-2.75a.75.75 0 0 0-1.06-.05" />
        </>
      )}
      {iconType === "change" && (
        <>
          <path d="M1.5 8c0 1.613.088 2.806.288 3.704.196.88.478 1.381.802 1.706s.826.607 1.706.802c.898.2 2.091.288 3.704.288s2.806-.088 3.704-.288c.88-.195 1.381-.478 1.706-.802s.607-.826.802-1.706c.2-.898.288-2.091.288-3.704s-.088-2.806-.288-3.704c-.195-.88-.478-1.381-.802-1.706s-.826-.606-1.706-.802C10.806 1.588 9.613 1.5 8 1.5s-2.806.088-3.704.288c-.88.196-1.381.478-1.706.802s-.606.826-.802 1.706C1.588 5.194 1.5 6.387 1.5 8M0 8c0-6.588 1.412-8 8-8s8 1.412 8 8-1.412 8-8 8-8-1.412-8-8m8 3a3 3 0 1 0 0-6 3 3 0 0 0 0 6" />
        </>
      )}
    </svg>
  );
}

function relativeTime(iso: string): string {
  const seconds = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 60) return "now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo`;
  return `${Math.floor(months / 12)}y`;
}

const CONVENTIONAL_COMMIT_RE = /^(\w+)(?:\(([^)]+)\))?(!)?:\s*(.+)$/;

const COMMIT_TYPE_COLORS: Record<string, string> = {
  feat:     "text-green-600 dark:text-green-400",
  fix:      "text-red-600 dark:text-red-400",
  docs:     "text-blue-600 dark:text-blue-400",
  style:    "text-purple-600 dark:text-purple-400",
  refactor: "text-amber-600 dark:text-amber-400",
  perf:     "text-orange-600 dark:text-orange-400",
  test:     "text-cyan-600 dark:text-cyan-400",
  chore:    "text-gray-500 dark:text-gray-400",
  ci:       "text-gray-500 dark:text-gray-400",
  build:    "text-gray-500 dark:text-gray-400",
};

interface ConventionalCommit {
  type: string;
  scope: string | null;
  isBreaking: boolean;
  description: string;
  color: string;
}

function parseConventionalCommit(message: string): ConventionalCommit | null {
  const match = message.match(CONVENTIONAL_COMMIT_RE);
  if (!match) return null;

  const type = match[1];
  const color = COMMIT_TYPE_COLORS[type];
  if (!color) return null;

  return {
    type,
    scope: match[2] ?? null,
    isBreaking: match[3] === "!",
    description: match[4],
    color,
  };
}

export function BranchDiff({ branchName, revision, commits, onExit }: BranchDiffProps) {
  const { repoId } = useRepo();
  const [diff, setDiff] = useState<BranchDiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [collapsedFiles, setCollapsedFiles] = useState<Record<string, boolean>>(
    {}
  );
  const [activeFileKey, setActiveFileKey] = useState<string | null>(null);
  const fileSectionRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const diffScrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      setLoading(true);
      setError(null);
      setDiff(null);
    });

    fetchBranchDiff(repoId, branchName)
      .then((result) => {
        if (!active) return;
        setDiff(result);
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : "Failed to load branch diff");
      })
      .finally(() => {
        if (!active) return;
        setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [branchName, revision, repoId]);

  const parsed = useMemo(() => {
    if (!diff?.patch.trim()) {
      return { files: [] as FileDiffMetadata[], parseError: null as string | null };
    }

    try {
      const files = parsePatchFiles(diff.patch)
        .flatMap((group) => group.files)
        .sort((a, b) => compareTreePaths(a.name, b.name));
      return { files, parseError: null as string | null };
    } catch (err) {
      return {
        files: [] as FileDiffMetadata[],
        parseError: err instanceof Error ? err.message : "Failed to parse patch",
      };
    }
  }, [diff]);

  const visibleCollapsedFiles = useMemo(() => {
    const next: Record<string, boolean> = {};
    parsed.files.forEach((file, index) => {
      const key = getFileKey(file, index);
      if (collapsedFiles[key]) next[key] = true;
    });
    return next;
  }, [collapsedFiles, parsed.files]);

  const fileKeys = useMemo(
    () => parsed.files.map((file, index) => getFileKey(file, index)),
    [parsed.files]
  );
  const selectedFileKey =
    activeFileKey && fileKeys.includes(activeFileKey)
      ? activeFileKey
      : (fileKeys[0] ?? null);

  const treePaths = useMemo(
    () => parsed.files.map((file) => file.name),
    [parsed.files]
  );

  const treeGitStatus = useMemo(
    () =>
      parsed.files.map((file) => ({
        path: file.name,
        status: mapDiffTypeToGitStatus(file.type),
      })),
    [parsed.files]
  );

  const parsedFilesRef = useRef(parsed.files);
  useEffect(() => {
    parsedFilesRef.current = parsed.files;
  }, [parsed.files]);

  const skipNextScroll = useRef(false);
  const suppressCount = useRef(0);
  const observerLockedUntil = useRef(0);

  const handleTreeSelection = useCallback((paths: readonly string[]) => {
    if (suppressCount.current > 0) {
      suppressCount.current -= 1;
      return;
    }
    const path = paths[0];
    if (!path) return;
    const files = parsedFilesRef.current;
    const index = files.findIndex((f) => f.name === path);
    if (index < 0) return;
    const key = getFileKey(files[index], index);
    setActiveFileKey(key);
    if (skipNextScroll.current) {
      skipNextScroll.current = false;
      return;
    }
    observerLockedUntil.current = performance.now() + 700;
    fileSectionRefs.current[key]?.scrollIntoView({
      behavior: "smooth",
      block: "start",
    });
  }, []);

  const { model: treeModel } = useFileTree({
    paths: [],
    initialExpansion: "open",
    icons: "standard",
    onSelectionChange: handleTreeSelection,
  });

  useEffect(() => {
    treeModel.resetPaths(treePaths);
    treeModel.setGitStatus(treeGitStatus);
    if (treePaths.length > 0) {
      skipNextScroll.current = true;
      suppressCount.current += 1;
      treeModel.getItem(treePaths[0])?.select();
    }
  }, [treeModel, treePaths, treeGitStatus]);

  useEffect(() => {
    if (!selectedFileKey) return;
    const idx = parsedFilesRef.current.findIndex(
      (f, i) => getFileKey(f, i) === selectedFileKey
    );
    if (idx < 0) return;
    const path = parsedFilesRef.current[idx].name;
    const current = treeModel.getSelectedPaths();
    if (current.length === 1 && current[0] === path) return;
    const toDeselect = current.filter((p) => p !== path);
    const shouldSelect = !current.includes(path);
    suppressCount.current += toDeselect.length + (shouldSelect ? 1 : 0);
    toDeselect.forEach((p) => treeModel.getItem(p)?.deselect());
    if (shouldSelect) treeModel.getItem(path)?.select();
  }, [selectedFileKey, treeModel]);

  useEffect(() => {
    if (parsed.files.length === 0) return;
    const diffScrollRoot = diffScrollRef.current;
    const observerRoot =
      diffScrollRoot && diffScrollRoot.scrollHeight > diffScrollRoot.clientHeight
        ? diffScrollRoot
        : null;

    const observer = new IntersectionObserver(
      (entries) => {
        if (performance.now() < observerLockedUntil.current) return;
        const visible = entries
          .filter((entry) => entry.isIntersecting)
          .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
        const key = visible?.target.getAttribute("data-file-key");
        if (key) setActiveFileKey(key);
      },
      {
        root: observerRoot,
        rootMargin: "-15% 0px -70% 0px",
        threshold: [0, 0.2, 0.6, 1],
      }
    );

    parsed.files.forEach((file, index) => {
      const key = getFileKey(file, index);
      const node = fileSectionRefs.current[key];
      if (node) observer.observe(node);
    });

    return () => {
      observer.disconnect();
    };
  }, [parsed.files]);

  const { repo } = useRepo();
  const { resolvedTheme } = useTheme();

  const diffOptions = useMemo(
    () => ({ ...BASE_DIFF_OPTIONS, themeType: resolvedTheme } as const),
    [resolvedTheme]
  );

  const fileCountLabel = `${parsed.files.length} file${parsed.files.length !== 1 ? "s" : ""}`;
  const showFileCount = !loading && !error && !parsed.parseError;
  const workspaceMode = Boolean(onExit);
  const contentLayoutClass = workspaceMode
    ? "flex min-h-0 flex-1 items-stretch gap-4"
    : "grid gap-3 lg:grid-cols-[18rem,minmax(0,1fr)] lg:items-start lg:gap-4";
  const explorerClass = workspaceMode
    ? "flex min-h-0 w-72 shrink-0 flex-col overflow-hidden rounded-md border bg-card"
    : "rounded-md border bg-card/60 lg:flex lg:max-h-[calc(100vh-14rem)] lg:min-h-0 lg:flex-col";
  const explorerListClass = workspaceMode
    ? "h-full overflow-y-auto p-2"
    : "max-h-56 overflow-auto p-2 lg:max-h-none lg:flex-1";
  const diffPaneClass = workspaceMode
    ? "min-h-0 min-w-0 overflow-y-auto pr-1"
    : "min-w-0 lg:max-h-[calc(100vh-14rem)] lg:overflow-y-auto lg:pr-1";
  const rootClassName = workspaceMode ? "flex h-full min-h-0 flex-col" : "space-y-2";

  return (
    <div className={rootClassName}>
      <div
        className={
          workspaceMode
            ? "mb-2 flex shrink-0 items-center justify-between gap-2 border-b px-1 pb-3"
            : "flex items-center justify-between gap-2"
        }
      >
        {workspaceMode ? (
          <>
            {showFileCount ? (
              <span className="text-xs text-muted-foreground">{fileCountLabel}</span>
            ) : (
              <span />
            )}
            <button
              onClick={onExit}
              className="inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground hover:bg-muted/50 hover:text-foreground transition-colors"
            >
              <X className="h-3.5 w-3.5" />
              Back to all stacks
            </button>
          </>
        ) : (
          <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
            Diff
          </h4>
        )}
        {!workspaceMode && showFileCount && (
          <span className="text-xs text-muted-foreground">{fileCountLabel}</span>
        )}
      </div>

      {loading && (
        <div className={contentLayoutClass}>
          <aside className={explorerClass}>
            <div className="px-3 py-2 text-xs font-medium text-muted-foreground">
              Files
            </div>
            <div className="p-2 space-y-1.5">
              {[0, 1, 2, 3, 4].map((i) => (
                <div key={i} className="flex items-center gap-2 px-1.5 py-1">
                  <div className="h-3 w-3 shrink-0 rounded bg-muted animate-pulse" />
                  <div
                    className="h-3 rounded bg-muted animate-pulse"
                    style={{ width: `${60 + (i * 17) % 30}%` }}
                  />
                </div>
              ))}
            </div>
          </aside>
          <div className={cn(diffPaneClass, "flex-1 space-y-3")}>
            {[0, 1, 2].map((i) => (
              <div key={i} className="rounded-md border bg-card/60 overflow-hidden">
                <div className="flex items-center gap-2 border-b px-3 py-2">
                  <div className="h-3 w-3 rounded bg-muted animate-pulse" />
                  <div
                    className="h-3 rounded bg-muted animate-pulse"
                    style={{ width: `${100 + (i * 37) % 80}px` }}
                  />
                </div>
                <div className="p-3 space-y-2">
                  {[0, 1, 2, 3].map((j) => (
                    <div
                      key={j}
                      className="h-3 rounded bg-muted/60 animate-pulse"
                      style={{ width: `${40 + ((i + j) * 23) % 55}%` }}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {!loading && error && (
        <p className="text-sm text-destructive">{error}</p>
      )}

      {!loading && !error && diff && !diff.patch.trim() && (
        <p className="text-sm text-muted-foreground">
          No changes relative to base commit {diff.baseRevision.slice(0, 7)}.
        </p>
      )}

      {!loading && !error && parsed.parseError && diff?.patch && (
        <div className="space-y-2">
          <p className="text-sm text-destructive">
            Could not parse diff. Showing raw patch.
          </p>
          <pre className="max-h-80 overflow-auto rounded-md border bg-muted/30 p-3 text-xs">
            {diff.patch}
          </pre>
        </div>
      )}

      {!loading && !error && !parsed.parseError && parsed.files.length > 0 && (
        <div className={contentLayoutClass}>
          <aside className={explorerClass}>
            {commits && commits.length > 0 && (
              <div className="border-b">
                <div className="px-3 py-2 text-xs font-medium text-muted-foreground">
                  Commits
                </div>
                <div className="px-2 pb-2 space-y-0.5">
                  {commits.map((commit) => {
                    const cc = parseConventionalCommit(commit.message);
                    const tooltip = `${commit.sha} — ${commit.message}`;

                    return (
                      <div
                        key={commit.sha}
                        className="flex items-center gap-1.5 rounded px-1.5 py-1 text-xs"
                      >
                        <GitCommitHorizontal className="h-3 w-3 shrink-0 text-muted-foreground" />
                        {cc ? (
                          <span className="flex items-baseline gap-1 min-w-0">
                            <span className={cn("shrink-0 font-medium", cc.color)}>
                              {cc.type}
                              {cc.scope ? `(${cc.scope})` : ""}
                              {cc.isBreaking ? "!" : ""}:
                            </span>
                            {repo ? (
                              <a
                                href={commitUrl(repo.owner, repo.repo, commit.sha)}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="truncate text-muted-foreground hover:text-foreground transition-colors"
                                title={tooltip}
                              >
                                {cc.description}
                              </a>
                            ) : (
                              <span className="truncate text-muted-foreground" title={tooltip}>
                                {cc.description}
                              </span>
                            )}
                          </span>
                        ) : repo ? (
                          <a
                            href={commitUrl(repo.owner, repo.repo, commit.sha)}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="truncate text-muted-foreground hover:text-foreground transition-colors"
                            title={tooltip}
                          >
                            {commit.message}
                          </a>
                        ) : (
                          <span className="truncate text-muted-foreground" title={tooltip}>
                            {commit.message}
                          </span>
                        )}
                        {commit.date && (
                          <span className="ml-auto shrink-0 text-muted-foreground/60 tabular-nums">
                            {relativeTime(commit.date)}
                          </span>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
            <div className="border-b px-3 py-2 text-xs font-medium text-muted-foreground">
              Files
            </div>
            <div className={explorerListClass}>
              <FileTree model={treeModel} style={{ height: "100%" }} />
            </div>
          </aside>

          <div ref={diffScrollRef} className={diffPaneClass}>
            <div className="space-y-3">
              {parsed.files.map((file, i) => {
                const fileKey = getFileKey(file, i);
                const collapsible = hasLineChanges(file);

                return (
                  <div
                    key={fileKey}
                    ref={(node) => {
                      fileSectionRefs.current[fileKey] = node;
                    }}
                    data-file-key={fileKey}
                    className={cn(
                      "relative rounded-md border bg-background scroll-mt-24",
                      selectedFileKey === fileKey && "ring-1 ring-primary/20"
                    )}
                  >
                    {collapsible && visibleCollapsedFiles[fileKey] ? (
                      <div className="flex items-center justify-between gap-3 px-3 py-2">
                        <div className="flex min-w-0 items-center gap-1.5">
                          {getChangeTypeIcon(file)}
                          <span
                            className="truncate font-mono text-xs text-muted-foreground"
                            title={getFileLabel(file)}
                          >
                            {getFileLabel(file)}
                          </span>
                        </div>
                        <button
                          onClick={() =>
                            setCollapsedFiles((prev) => ({
                              ...prev,
                              [fileKey]: false,
                            }))
                          }
                          className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs text-muted-foreground hover:bg-muted/50 hover:text-foreground transition-colors"
                        >
                          <ChevronRight className="h-3.5 w-3.5" />
                          Expand
                        </button>
                      </div>
                    ) : (
                      <>
                        {collapsible && (
                          <button
                            onClick={() =>
                              setCollapsedFiles((prev) => ({
                                ...prev,
                                [fileKey]: true,
                              }))
                            }
                            className="absolute right-2 top-2 z-10 inline-flex items-center gap-1 rounded-md border bg-background/90 px-2 py-1 text-xs text-muted-foreground hover:bg-muted/70 hover:text-foreground transition-colors"
                          >
                            <ChevronDown className="h-3.5 w-3.5" />
                            Collapse
                          </button>
                        )}
                        <FileDiff fileDiff={file} options={diffOptions} className="block" />
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
