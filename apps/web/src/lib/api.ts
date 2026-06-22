// API client for stackit-web server

// Empty default = same-origin requests. The embedded production build is
// served from the Go server itself, so relative URLs hit the right host
// without baking it in at build time. Set NEXT_PUBLIC_API_URL when running
// `next dev` against a separate Go server on another port.
const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

// CSRF_HEADER must be sent on every non-safe request. The server doesn't
// inspect the value, only the header's presence — see
// internal/api/auth/middleware.go (RequireCSRFHeader).
const CSRF_HEADER = "X-Stackit-CSRF";
const CSRF_HEADER_VALUE = "1";

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

// --- Server Capabilities ---

// ConfigResponse mirrors httpcontract.ConfigResponse. It advertises server
// capabilities so the UI can adapt at runtime instead of relying on
// build-time flags.
export interface ConfigResponse {
  readOnly: boolean;
  authRequired: boolean;
}

// --- Auth Types ---

export interface MeResponse {
  login: string;
  id: number;
}

// UnauthorizedError is thrown by fetchAPI on a 401. Callers (the auth
// provider) translate it into a redirect to /auth/login; other consumers
// can treat it as a fatal error.
export class UnauthorizedError extends Error {
  constructor() {
    super("unauthenticated");
    this.name = "UnauthorizedError";
  }
}

// --- Fetch Functions ---

async function fetchAPI<T>(path: string): Promise<T> {
  // credentials: include sends the session cookie cross-origin during
  // `next dev` (web on :3000, API on :8080). The server's CORS layer
  // sets Allow-Credentials: true for configured origins.
  const res = await fetch(`${API_BASE}${path}`, { credentials: "include" });
  if (res.status === 401) {
    throw new UnauthorizedError();
  }
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

export function fetchMe(): Promise<MeResponse> {
  return fetchAPI<MeResponse>("/auth/me");
}

// fetchConfig reads server capabilities. It is served unauthenticated, so
// the client can call it before deciding whether to attempt a login.
export function fetchConfig(): Promise<ConfigResponse> {
  return fetchAPI<ConfigResponse>("/api/v1/config");
}

export function authLoginURL(returnTo?: string): string {
  const path = "/auth/login";
  if (!returnTo) {
    return `${API_BASE}${path}`;
  }
  return `${API_BASE}${path}?return=${encodeURIComponent(returnTo)}`;
}

export async function logout(): Promise<void> {
  const res = await fetch(`${API_BASE}/auth/logout`, {
    method: "POST",
    credentials: "include",
    headers: { [CSRF_HEADER]: CSRF_HEADER_VALUE },
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(`logout failed: ${res.status}`);
  }
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
    {
      method: "POST",
      credentials: "include",
      headers: { [CSRF_HEADER]: CSRF_HEADER_VALUE },
    }
  );
  if (res.status === 401) {
    throw new UnauthorizedError();
  }
  const data: SubmitResponse = await res.json();
  if (!res.ok && !data.message) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return data;
}
