import { describe, expect, it } from "vitest";
import type { BranchResponse, StackDetail } from "@/lib/api";
import { filterStacks } from "@/lib/stack-search";

function branch(name: string): BranchResponse {
  return {
    name, depth: 0, isCurrent: false, needsRestack: false,
    isLocked: false, isFrozen: false, revision: "abc123",
    commitDate: "2026-09-08", commitAuthor: "Alice", commitCount: 1,
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

  it.each(["feat/login", "ACCOUNT", "sign in", "123", "#123", "ALICE", "@alice", "web"])(
    "finds stacks by %j without removing parent branches", (query) => {
      expect(filterStacks(stacks, query)).toEqual([stacks[0]]);
      expect(filterStacks(stacks, query)[0].branches).toHaveLength(2);
    }
  );

  it("matches multiple terms across stack and branch metadata", () => {
    expect(filterStacks(stacks, "  alice  sign  #123 ")).toEqual([stacks[0]]);
    expect(filterStacks(stacks, "alice cache")).toEqual([]);
  });

  it("handles stacks without owners or PRs", () => {
    expect(filterStacks(stacks, "cache")).toEqual([stacks[1]]);
    expect(filterStacks(stacks, "missing")).toEqual([]);
    expect(filterStacks([], "cache")).toEqual([]);
  });
});
