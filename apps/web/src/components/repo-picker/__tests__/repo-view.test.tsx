import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { StackDetail } from "@/lib/api";
import { RepoView } from "@/components/repo-picker/repo-view";

const { useRepo } = vi.hoisted(() => ({ useRepo: vi.fn() }));
vi.mock("@/components/providers/repo-provider", () => ({ useRepo }));
vi.mock("next/dynamic", () => ({ default: () => () => null }));
vi.mock("@/components/layout/header", () => ({ Header: () => null }));
vi.mock("@/components/layout/event-feed", () => ({ EventFeed: () => null }));
vi.mock("@/components/ui/background-mesh", () => ({ BackgroundMesh: () => null }));
vi.mock("@/components/branch-detail/branch-detail", () => ({ BranchDetail: () => null }));
vi.mock("@/components/recently-merged/recently-merged", () => ({
  RecentlyMerged: () => <div>Commit history</div>,
}));
vi.mock("@/components/swimlane/owner-swimlane", () => ({
  OwnerSwimlane: ({ stacks, onSelectStack }: {
    stacks: StackDetail[];
    onSelectStack: (stack: StackDetail) => void;
  }) => stacks.map((stack) => (
    <button key={stack.rootBranch} onClick={() => onSelectStack(stack)}>
      {stack.title}
    </button>
  )),
}));
vi.mock("@/components/branch-detail/stack-detail", () => ({
  StackDetailPanel: ({ stack }: { stack: StackDetail }) => <div>Selected: {stack.title}</div>,
}));

const stacks: StackDetail[] = [
  {
    rootBranch: "feat/login", title: "Account access", owner: "alice",
    status: "pending", branchCount: 0, prCount: 0, isCurrent: false, branches: [],
  },
  {
    rootBranch: "fix/cache", title: "Cache refresh", owner: "bob",
    status: "shippable", branchCount: 0, prCount: 0, isCurrent: false, branches: [],
  },
];

beforeEach(() => {
  window.history.replaceState({}, "", "/example/repo");
  useRepo.mockReturnValue({
    repo: { owner: "example", repo: "repo", trunk: "main", currentUser: "alice" },
    stackDetails: stacks, loading: false, error: null, refresh: vi.fn(),
  });
});

describe("RepoView search", () => {
  it("filters swimlanes and restores stacks after clearing an unmatched search", () => {
    render(<RepoView />);
    const search = screen.getByRole("searchbox", { name: "Search stacks" });
    fireEvent.change(search, { target: { value: "@bob" } });
    expect(screen.queryByRole("button", { name: "Account access" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cache refresh" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("1 of 2 stacks");

    fireEvent.change(search, { target: { value: "missing" } });
    expect(screen.getByText("No stacks match your search.")).toBeInTheDocument();
    expect(screen.getByText("Commit history")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    expect(search).toHaveValue("");
    expect(screen.getByRole("status")).toHaveTextContent("2 stacks");
    expect(screen.getByRole("button", { name: "Account access" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cache refresh" })).toBeInTheDocument();
  });

  it("keeps selected stack details open when search hides its swimlane", () => {
    render(<RepoView />);
    fireEvent.click(screen.getByRole("button", { name: "Account access" }));
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "cache" } });
    expect(screen.getByText("Selected: Account access")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Account access" })).not.toBeInTheDocument();
  });

  it("clears search with Escape while retaining keyboard focus", () => {
    render(<RepoView />);
    const search = screen.getByRole("searchbox");
    search.focus();
    fireEvent.change(search, { target: { value: "cache" } });
    fireEvent.keyDown(search, { key: "Escape" });
    expect(search).toHaveValue("");
    expect(search).toHaveFocus();
    expect(screen.getByRole("button", { name: "Account access" })).toBeInTheDocument();
  });

  it("returns to the overview when details are closed", () => {
    render(<RepoView />);
    fireEvent.click(screen.getByRole("button", { name: "Account access" }));
    fireEvent.click(screen.getByRole("button", { name: "Close details" }));
    expect(screen.queryByText("Selected: Account access")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Overview" })).toBeInTheDocument();
  });
});
