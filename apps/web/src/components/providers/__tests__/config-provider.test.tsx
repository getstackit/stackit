import { describe, it, expect, vi, beforeEach, type Mock } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { ConfigProvider } from "../config-provider";
import { ReadOnlyBanner } from "@/components/read-only-banner";
import { fetchConfig, type ConfigResponse } from "@/lib/api";

vi.mock("@/lib/api", () => ({
  fetchConfig: vi.fn(),
}));

const mockFetchConfig = fetchConfig as Mock;

const WRITABLE_FALLBACK: ConfigResponse = { readOnly: false, authRequired: true };

beforeEach(() => {
  mockFetchConfig.mockReset();
});

describe("ConfigProvider + ReadOnlyBanner", () => {
  it("shows the banner when the server reports read-only", async () => {
    mockFetchConfig.mockResolvedValue({ readOnly: true, authRequired: false });

    render(
      <ConfigProvider fallback={WRITABLE_FALLBACK}>
        <ReadOnlyBanner />
      </ConfigProvider>
    );

    expect(await screen.findByRole("status")).toHaveTextContent(/read-only/i);
  });

  it("hides the banner on a writable server", async () => {
    mockFetchConfig.mockResolvedValue({ readOnly: false, authRequired: true });

    render(
      <ConfigProvider fallback={WRITABLE_FALLBACK}>
        <ReadOnlyBanner />
      </ConfigProvider>
    );

    await waitFor(() => expect(screen.queryByText("Loading…")).not.toBeInTheDocument());
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("falls back to the supplied defaults when /config is unreachable", async () => {
    mockFetchConfig.mockRejectedValue(new Error("network"));

    render(
      <ConfigProvider fallback={{ readOnly: true, authRequired: false }}>
        <ReadOnlyBanner />
      </ConfigProvider>
    );

    // The fallback marks the server read-only, so the banner appears even
    // though the fetch failed.
    expect(await screen.findByRole("status")).toBeInTheDocument();
  });
});
