import { describe, expect, it } from "vitest";
import type { BranchResponse, StackDetail, TrunkCommitResponse } from "@/lib/api";
import { filterRecentCommits, filterStacks } from "@/lib/stack-search";

function branch(name: string): BranchResponse {
  return {
    name, depth: 0, isCurrent: false, needsRestack: false,
    isLocked: false, isFrozen: false, revision: "abc123",
    commitDate: "2026-09-08", commitAuthor: name === "fix/cache" ? "Bob" : "Alice", commitCount: 1,
    linesAdded: 0, linesDeleted: 0,
  };
}

const stacks: StackDetail[] = [
  {
    rootBranch: "feat/auth", title: "Account access", owner: "alice", scope: "web",
    status: "pending", branchCount: 2, prCount: 1, isCurrent: false,
    branches: [
      branch("feat/auth"),
      {
        ...branch("feat/login"), parent: "feat/auth",
        commits: [{ sha: "def456", message: "Validate session tokens" }],
        pr: {
          number: 123, title: "Add sign in", state: "OPEN", isDraft: false,
          url: "https://github.com/example/repo/pull/123", base: "feat/auth",
        },
      },
    ],
  },
  {
    rootBranch: "fix/cache", title: "Cache refresh", status: "shippable",
    branchCount: 1, prCount: 0, isCurrent: true, branches: [branch("fix/cache")],
  },
];

describe("filterStacks", () => {
  it.each(["", "  \n  "])("shows all stacks for a blank query %j", (query) => {
    expect(filterStacks(stacks, query)).toEqual(stacks);
  });

  it.each(["feat/login", "ACCOUNT", "sign in", "#123", "ALICE", "@alice", "web", "session tokens", "def456"])(
    "finds stacks by %j without removing parent branches", (query) => {
      expect(filterStacks(stacks, query)).toEqual([stacks[0]]);
      expect(filterStacks(stacks, query)[0].branches).toHaveLength(2);
    }
  );

  it("matches multiple terms across stack and branch metadata", () => {
    expect(filterStacks(stacks, "  alice  sign  #123 ")).toEqual([stacks[0]]);
    expect(filterStacks(stacks, "alice cache")).toEqual([]);
  });

  it("matches bare numbers in hashes as well as PR numbers", () => {
    expect(filterStacks(stacks, "123")).toEqual(stacks);
    expect(filterStacks(stacks, "#123")).toEqual([stacks[0]]);
  });

  it("handles stacks without owners or PRs", () => {
    expect(filterStacks(stacks, "cache")).toEqual([stacks[1]]);
    expect(filterStacks(stacks, "missing")).toEqual([]);
    expect(filterStacks([], "cache")).toEqual([]);
  });
});

describe("filterRecentCommits", () => {
  const commits: TrunkCommitResponse[] = [
    { sha: "abc123", message: "Fix cache invalidation", author: "Bob", date: "2026-09-09", kind: "regular", prNumber: 42 },
    { sha: "def456", message: "Ship authentication", author: "Alice", date: "2026-09-08", kind: "stack-merge", stackPRs: [123, 124], stackPRTitles: { 124: "Validate session tokens" }, stackScope: "web" },
  ];

  it("preserves history for an empty query", () => {
    expect(filterRecentCommits(commits, "  ")).toEqual(commits);
  });

  it.each(["cache", "ABC123", "@bob", "#42", "bob cache"])("searches recent commits by %j", (query) => {
    expect(filterRecentCommits(commits, query)).toEqual([commits[0]]);
  });

  it.each(["#124", "session tokens", "web"])("searches merged stack metadata by %j", (query) => {
    expect(filterRecentCommits(commits, query)).toEqual([commits[1]]);
  });

  it("requires all terms to match within one commit", () => {
    expect(filterRecentCommits(commits, "cache alice")).toEqual([]);
  });
});
