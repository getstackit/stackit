import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  fetchView,
  fetchRepo,
  fetchRepos,
  fetchStacks,
  fetchStack,
  fetchBranch,
  fetchBranchDiff,
} from "../api";

const mockFetch = vi.fn();
global.fetch = mockFetch;

beforeEach(() => {
  mockFetch.mockReset();
});

function mockOk(data: unknown) {
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve(data),
  });
}

function mockError(status: number, statusText: string) {
  mockFetch.mockResolvedValueOnce({
    ok: false,
    status,
    statusText,
  });
}

describe("fetchRepos", () => {
  it("fetches the unscoped repos index", async () => {
    const data = { repos: [{ id: "stackit", displayName: "Stackit", trunk: "main" }] };
    mockOk(data);

    const result = await fetchRepos();
    expect(result).toEqual(data);
    expect(mockFetch).toHaveBeenCalledWith("http://localhost:8080/api/v1/repos", {
      credentials: "include",
    });
  });
});

describe("fetchView", () => {
  it("scopes to repoId", async () => {
    const data = { repo: {}, stacks: [] };
    mockOk(data);

    const result = await fetchView("stackit");
    expect(result).toEqual(data);
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/repos/stackit/view",
      { credentials: "include" }
    );
  });
});

describe("fetchRepo", () => {
  it("scopes to repoId", async () => {
    const data = { owner: "test", repo: "repo", trunk: "main", currentBranch: "main", remote: "origin" };
    mockOk(data);

    const result = await fetchRepo("stackit");
    expect(result).toEqual(data);
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/repos/stackit/repo",
      { credentials: "include" }
    );
  });
});

describe("fetchStacks", () => {
  it("scopes to repoId", async () => {
    mockOk([]);
    const result = await fetchStacks("stackit");
    expect(result).toEqual([]);
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/repos/stackit/stacks",
      { credentials: "include" }
    );
  });
});

describe("fetchStack", () => {
  it("encodes both repoId and branch name in URL", async () => {
    mockOk({ rootBranch: "feat/foo", branches: [] });

    await fetchStack("stackit", "feat/foo");
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/repos/stackit/stacks/feat%2Ffoo",
      { credentials: "include" }
    );
  });
});

describe("fetchBranch", () => {
  it("encodes branch name in URL", async () => {
    mockOk({ name: "feat/bar" });

    await fetchBranch("stackit", "feat/bar");
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/repos/stackit/branches/feat%2Fbar",
      { credentials: "include" }
    );
  });
});

describe("fetchBranchDiff", () => {
  it("sends encoded branch name in query string", async () => {
    mockOk({ branch: "feat/bar", baseRevision: "abc", headRevision: "def", patch: "" });

    await fetchBranchDiff("stackit", "feat/bar");
    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/repos/stackit/branch-diff?branch=feat%2Fbar",
      { credentials: "include" }
    );
  });
});

describe("error handling", () => {
  it("throws on non-ok response", async () => {
    mockError(404, "Not Found");

    await expect(fetchRepo("stackit")).rejects.toThrow("API error: 404 Not Found");
  });

  it("throws on 500 response", async () => {
    mockError(500, "Internal Server Error");

    await expect(fetchView("stackit")).rejects.toThrow("API error: 500 Internal Server Error");
  });
});
