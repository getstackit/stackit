// API client for stackit-web server

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

// --- Response Types (matching Go API types) ---

export interface RepoResponse {
  owner: string;
  repo: string;
  trunk: string;
  currentBranch: string;
  remote: string;
  currentUser?: string;
}

export interface RepoSummary {
  id: string;
  displayName: string;
  trunk: string;
  currentBranch?: string;
}

export interface ReposListResponse {
  repos: RepoSummary[];
}

export interface StackSummary {
  rootBranch: string;
  title: string;
  status: "shippable" | "pending" | "blocked" | "incomplete";
  scope?: string;
  branchCount: number;
  prCount: number;
  isCurrent: boolean;
  hasWorktree?: boolean;
  description?: string;
  owner?: string;
}

export interface StackDetail extends StackSummary {
  branches: BranchResponse[];
}

export interface BranchResponse {
  name: string;
  parent?: string;
  children?: string[];
  depth: number;
  isCurrent: boolean;
  needsRestack: boolean;
  isLocked: boolean;
  lockReason?: string;
  isFrozen: boolean;
  scope?: string;
  revision: string;
  commitDate: string;
  commitAuthor: string;
  commitCount: number;
  linesAdded: number;
  linesDeleted: number;
  commits?: CommitResponse[];
  pr?: PRResponse;
  ci?: CIResponse;
  remoteStatus?: RemoteStatus;
}

export interface BranchDiffResponse {
  branch: string;
  baseRevision: string;
  headRevision: string;
  patch: string;
}

export interface PRResponse {
  number: number;
  title: string;
  state: "OPEN" | "MERGED" | "CLOSED";
  url: string;
  isDraft: boolean;
  base: string;
}

export interface CIResponse {
  status: "passing" | "failing" | "pending" | "none";
  reviewDecision: string;
  checks?: CheckDetailResponse[];
}

export interface CheckDetailResponse {
  name: string;
  status: string;
  conclusion: string;
}

export interface CommitResponse {
  sha: string;
  message: string;
  date?: string;
}

export interface RemoteStatus {
  ahead: boolean;
  behind: boolean;
  diverged: boolean;
  missingRemote: boolean;
}

export interface TrunkCommitResponse {
  sha: string;
  message: string;
  author: string;
  date: string;
  kind: "regular" | "stack-merge";
  prNumber?: number;
  stackSize?: number;
  stackPRs?: number[];
  stackPRTitles?: Record<number, string>;
  stackScope?: string;
}

// --- Combined View Response ---

export interface ViewResponse {
  repo: RepoResponse;
  stacks: StackDetail[];
  recentlyMerged?: TrunkCommitResponse[];
}

// --- Fetch Functions ---

async function fetchAPI<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`);
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

function repoPath(repoId: string, suffix: string): string {
  return `/api/v1/repos/${encodeURIComponent(repoId)}/${suffix}`;
}

export function fetchRepos(): Promise<ReposListResponse> {
  return fetchAPI<ReposListResponse>("/api/v1/repos");
}

export function fetchView(repoId: string): Promise<ViewResponse> {
  return fetchAPI<ViewResponse>(repoPath(repoId, "view"));
}

export function fetchRepo(repoId: string): Promise<RepoResponse> {
  return fetchAPI<RepoResponse>(repoPath(repoId, "repo"));
}

export function fetchStacks(repoId: string): Promise<StackSummary[]> {
  return fetchAPI<StackSummary[]>(repoPath(repoId, "stacks"));
}

export function fetchStack(repoId: string, rootBranch: string): Promise<StackDetail> {
  return fetchAPI<StackDetail>(repoPath(repoId, `stacks/${encodeURIComponent(rootBranch)}`));
}

export function fetchBranches(repoId: string): Promise<BranchResponse[]> {
  return fetchAPI<BranchResponse[]>(repoPath(repoId, "branches"));
}

export function fetchBranch(repoId: string, name: string): Promise<BranchResponse> {
  return fetchAPI<BranchResponse>(repoPath(repoId, `branches/${encodeURIComponent(name)}`));
}

export function fetchBranchDiff(repoId: string, name: string): Promise<BranchDiffResponse> {
  return fetchAPI<BranchDiffResponse>(
    repoPath(repoId, `branch-diff?branch=${encodeURIComponent(name)}`)
  );
}

// --- Event Feed Types ---

export type EventKind =
  | "branch_created"
  | "branch_deleted"
  | "branch_switched"
  | "pr_opened"
  | "pr_merged"
  | "pr_closed"
  | "ci_changed"
  | "needs_restack"
  | "restack_resolved"
  | "stack_created"
  | "stack_deleted"
  | "revision_updated";

export interface FeedEvent {
  kind: EventKind;
  timestamp: string;
  branch?: string;
  detail?: string;
}

// --- Mutation Types ---

export interface SubmitResponse {
  success: boolean;
  message: string;
  branches?: {
    name: string;
    status: string;
    url?: string;
    error?: string;
  }[];
}

// --- Mutation Functions ---

export async function submitStack(repoId: string, rootBranch: string): Promise<SubmitResponse> {
  const res = await fetch(
    `${API_BASE}${repoPath(repoId, `stacks/${encodeURIComponent(rootBranch)}/submit`)}`,
    { method: "POST" }
  );
  const data: SubmitResponse = await res.json();
  if (!res.ok && !data.message) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return data;
}
