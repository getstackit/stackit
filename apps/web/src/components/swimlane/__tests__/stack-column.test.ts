import { describe, it, expect } from "vitest";
import { hasBranching, orderBranches } from "../stack-column";
import { shortenBranchName } from "@/lib/branch-utils";
import type { BranchResponse } from "@/lib/api";

function makeBranch(overrides: Partial<BranchResponse> & { name: string }): BranchResponse {
  return {
    depth: 0,
    isCurrent: false,
    needsRestack: false,
    isLocked: false,
    isFrozen: false,
    revision: "abc123",
    commitDate: "2025-01-01",
    commitAuthor: "test",
    commitCount: 1,
    linesAdded: 0,
    linesDeleted: 0,
    ...overrides,
  };
}

describe("hasBranching", () => {
  it("returns false for empty branches", () => {
    expect(hasBranching([])).toBe(false);
  });

  it("returns false for a single branch with no children", () => {
    expect(hasBranching([makeBranch({ name: "main" })])).toBe(false);
  });

  it("returns false for a linear stack", () => {
    const branches = [
      makeBranch({ name: "main", children: ["feat-a"] }),
      makeBranch({ name: "feat-a", parent: "main", children: ["feat-b"] }),
      makeBranch({ name: "feat-b", parent: "feat-a" }),
    ];
    expect(hasBranching(branches)).toBe(false);
  });

  it("returns true when a branch has two children", () => {
    const branches = [
      makeBranch({ name: "main", children: ["feat-a", "feat-b"] }),
      makeBranch({ name: "feat-a", parent: "main" }),
      makeBranch({ name: "feat-b", parent: "main" }),
    ];
    expect(hasBranching(branches)).toBe(true);
  });

  it("returns true when a non-root branch has multiple children", () => {
    const branches = [
      makeBranch({ name: "main", children: ["middle"] }),
      makeBranch({ name: "middle", parent: "main", children: ["left", "right"] }),
      makeBranch({ name: "left", parent: "middle" }),
      makeBranch({ name: "right", parent: "middle" }),
    ];
    expect(hasBranching(branches)).toBe(true);
  });

  it("returns false when children is undefined", () => {
    const branches = [
      makeBranch({ name: "main" }),
      makeBranch({ name: "feat", parent: "main" }),
    ];
    expect(hasBranching(branches)).toBe(false);
  });

  it("returns false when children is an empty array", () => {
    const branches = [
      makeBranch({ name: "main", children: [] }),
    ];
    expect(hasBranching(branches)).toBe(false);
  });
});

describe("shortenBranchName", () => {
  it("returns description part from user/timestamp/description pattern", () => {
    expect(shortenBranchName("jonnii/20260301202047/show-PR-titles")).toBe("show-PR-titles");
  });

  it("returns full name when no timestamp pattern", () => {
    expect(shortenBranchName("feature/my-branch")).toBe("feature/my-branch");
  });

  it("returns full name for simple branch names", () => {
    expect(shortenBranchName("main")).toBe("main");
  });

  it("handles description with slashes", () => {
    expect(shortenBranchName("user/20260301202047/feat/nested")).toBe("feat/nested");
  });
});

describe("orderBranches", () => {
  const names = (branches: BranchResponse[]) => orderBranches(branches).map((b) => b.name);

  it("orders a linear stack root to leaf regardless of input order", () => {
    const branches = [
      makeBranch({ name: "c", parent: "b" }),
      makeBranch({ name: "a", parent: "main" }),
      makeBranch({ name: "b", parent: "a" }),
    ];
    expect(names(branches)).toEqual(["a", "b", "c"]);
  });

  it("finishes each subtree before the next sibling, keeping sibling order", () => {
    const branches = [
      makeBranch({ name: "root" }),
      makeBranch({ name: "right", parent: "root" }),
      makeBranch({ name: "left", parent: "root" }),
      makeBranch({ name: "right-child", parent: "right" }),
      makeBranch({ name: "left-child", parent: "left" }),
    ];
    expect(names(branches)).toEqual(["root", "right", "right-child", "left", "left-child"]);
  });

  it("treats a branch whose parent is not listed as a root", () => {
    const branches = [
      makeBranch({ name: "orphan", parent: "landed" }),
      makeBranch({ name: "orphan-child", parent: "orphan" }),
      makeBranch({ name: "other" }),
    ];
    expect(names(branches)).toEqual(["orphan", "orphan-child", "other"]);
  });

  it("includes every branch exactly once", () => {
    const branches = Array.from({ length: 200 }, (_, i) =>
      makeBranch({ name: `b${i}`, parent: i === 0 ? undefined : `b${Math.floor((i - 1) / 3)}` }),
    ).reverse();
    const ordered = names(branches);
    expect(ordered).toHaveLength(200);
    expect(new Set(ordered).size).toBe(200);
    expect(ordered[0]).toBe("b0");
  });
});
