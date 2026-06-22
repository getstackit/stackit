import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { AddRepository } from "../add-repository";

let mockReadOnly = false;
vi.mock("@/components/providers/config-provider", () => ({
  useConfig: () => ({ readOnly: mockReadOnly, authRequired: true }),
}));

const mockFetch = vi.fn();
global.fetch = mockFetch;

beforeEach(() => {
  mockFetch.mockReset();
  mockReadOnly = false;
});

describe("AddRepository", () => {
  it("posts owner/name and calls onAdded with the new repo", async () => {
    const repo = { id: "octo-widget", displayName: "octo/widget", trunk: "main" };
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 201,
      json: () => Promise.resolve(repo),
    });
    const onAdded = vi.fn();

    render(<AddRepository onAdded={onAdded} />);
    fireEvent.change(screen.getByLabelText("Repository owner"), {
      target: { value: "octo" },
    });
    fireEvent.change(screen.getByLabelText("Repository name"), {
      target: { value: "widget" },
    });
    fireEvent.click(screen.getByRole("button", { name: /add/i }));

    await waitFor(() => expect(onAdded).toHaveBeenCalledWith(repo));

    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toContain("/api/v1/repos");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ owner: "octo", name: "widget" });
  });

  it("surfaces the server error and does not call onAdded", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 404,
      json: () => Promise.resolve({ error: "repository not found or not accessible" }),
    });
    const onAdded = vi.fn();

    render(<AddRepository onAdded={onAdded} />);
    fireEvent.change(screen.getByLabelText("Repository owner"), {
      target: { value: "octo" },
    });
    fireEvent.change(screen.getByLabelText("Repository name"), {
      target: { value: "ghost" },
    });
    fireEvent.click(screen.getByRole("button", { name: /add/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "repository not found or not accessible"
    );
    expect(onAdded).not.toHaveBeenCalled();
  });

  it("renders nothing on a read-only server", () => {
    mockReadOnly = true;
    const { container } = render(<AddRepository onAdded={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("disables Add until both fields are filled", () => {
    render(<AddRepository onAdded={vi.fn()} />);
    const button = screen.getByRole("button", { name: /add/i });
    expect(button).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Repository owner"), {
      target: { value: "octo" },
    });
    expect(button).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Repository name"), {
      target: { value: "widget" },
    });
    expect(button).not.toBeDisabled();
  });
});
