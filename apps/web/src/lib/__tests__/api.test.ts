import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  fetchView,
  fetchRepo,
  fetchRepos,
  fetchStacks,
  fetchStack,
  fetchBranch,
  fetchBranchDiff,
  fetchConfig,
  onboardRepo,
  UnauthorizedError,
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
    expect(mockFetch).toHaveBeenCalledWith("/api/v1/repos", {
      credentials: "include",
    });
  });
});

describe("onboardRepo", () => {
  it("POSTs owner/name with the CSRF header and returns the summary", async () => {
    const repo = { id: "octo-widget", displayName: "octo/widget", trunk: "main" };
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 201,
      json: () => Promise.resolve(repo),
    });

    const result = await onboardRepo("octo", "widget");
    expect(result).toEqual(repo);

    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe("/api/v1/repos");
    expect(init.method).toBe("POST");
    expect(init.headers["X-Stackit-CSRF"]).toBe("1");
    expect(JSON.parse(init.body)).toEqual({ owner: "octo", name: "widget" });
  });

  it("throws the server's error message on failure", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: "Not Found",
      json: () => Promise.resolve({ error: "repository not found or not accessible" }),
    });

    await expect(onboardRepo("octo", "ghost")).rejects.toThrow(
      "repository not found or not accessible"
    );
  });

  it("throws UnauthorizedError on 401", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 401,
      statusText: "Unauthorized",
      json: () => Promise.resolve({}),
    });

    await expect(onboardRepo("octo", "widget")).rejects.toBeInstanceOf(
      UnauthorizedError
    );
  });
});

describe("fetchView", () => {
  it("scopes to repoId", async () => {
    const data = { repo: {}, stacks: [] };
    mockOk(data);

    const result = await fetchView("stackit");
    expect(result).toEqual(data);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/v1/repos/stackit/view",
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
      "/api/v1/repos/stackit/repo",
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
      "/api/v1/repos/stackit/stacks",
      { credentials: "include" }
    );
  });
});

describe("fetchStack", () => {
  it("encodes both repoId and branch name in URL", async () => {
    mockOk({ rootBranch: "feat/foo", branches: [] });

    await fetchStack("stackit", "feat/foo");
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/v1/repos/stackit/stacks/feat%2Ffoo",
      { credentials: "include" }
    );
  });
});

describe("fetchBranch", () => {
  it("encodes branch name in URL", async () => {
    mockOk({ name: "feat/bar" });

    await fetchBranch("stackit", "feat/bar");
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/v1/repos/stackit/branches/feat%2Fbar",
      { credentials: "include" }
    );
  });
});

describe("fetchBranchDiff", () => {
  it("sends encoded branch name in query string", async () => {
    mockOk({ branch: "feat/bar", baseRevision: "abc", headRevision: "def", patch: "" });

    await fetchBranchDiff("stackit", "feat/bar");
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/v1/repos/stackit/branch-diff?branch=feat%2Fbar",
      { credentials: "include" }
    );
  });
});

describe("fetchConfig", () => {
  it("reads the unauthenticated capabilities endpoint", async () => {
    const data = { readOnly: true, authRequired: false };
    mockOk(data);

    const result = await fetchConfig();
    expect(result).toEqual(data);
    expect(mockFetch).toHaveBeenCalledWith("/api/v1/config", {
      credentials: "include",
    });
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
