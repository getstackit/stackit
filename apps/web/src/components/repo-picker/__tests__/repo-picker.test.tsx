import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { RepoPicker } from "../repo-picker";

const mockPush = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
}));

const mockFetch = vi.fn();
global.fetch = mockFetch;

beforeEach(() => {
  mockFetch.mockReset();
  mockPush.mockReset();
});

function mockRepos(data: unknown) {
  mockFetch.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve(data),
  });
}

describe("RepoPicker", () => {
  it("renders a card for every registered repo", async () => {
    mockRepos({
      repos: [
        { id: "stackit", displayName: "Stackit", trunk: "main", currentBranch: "feature/x" },
        { id: "other", displayName: "Other Project", trunk: "main" },
      ],
    });

    render(<RepoPicker />);

    expect(await screen.findByTestId("repo-card-stackit")).toBeDefined();
    expect(screen.getByTestId("repo-card-other")).toBeDefined();
    expect(screen.getByText("Stackit")).toBeDefined();
    expect(screen.getByText("Other Project")).toBeDefined();
  });

  it("navigates to /{repoId} on click", async () => {
    mockRepos({
      repos: [{ id: "stackit", displayName: "Stackit", trunk: "main" }],
    });

    render(<RepoPicker />);

    const card = await screen.findByTestId("repo-card-stackit");
    fireEvent.click(card);
    expect(mockPush).toHaveBeenCalledWith("/stackit");
  });

  it("shows the empty-state message when no repos are configured", async () => {
    mockRepos({ repos: [] });

    render(<RepoPicker />);

    await waitFor(() => {
      expect(screen.getByText(/No repositories configured/i)).toBeDefined();
    });
  });

  it("renders an error message when the fetch fails", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
    });

    render(<RepoPicker />);

    await waitFor(() => {
      expect(screen.getByText(/API error: 500/)).toBeDefined();
    });
  });
});
