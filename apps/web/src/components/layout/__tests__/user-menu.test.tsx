import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { UserMenu } from "../user-menu";

const useAuthMock = vi.fn();

vi.mock("@/components/providers/auth-provider", () => ({
  useAuth: () => useAuthMock(),
}));

const logout = vi.fn();

function authValue(overrides: Record<string, unknown> = {}) {
  return {
    user: { login: "octocat", id: 1 },
    disabled: false,
    loginURL: "/auth/login",
    isLoading: false,
    logout,
    ...overrides,
  };
}

describe("UserMenu", () => {
  beforeEach(() => {
    useAuthMock.mockReset();
    logout.mockReset();
  });

  it("renders nothing when there is no user", () => {
    useAuthMock.mockReturnValue(authValue({ user: null }));
    const { container } = render(<UserMenu />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the signed-in login and avatar initial in the trigger", () => {
    useAuthMock.mockReturnValue(authValue());
    render(<UserMenu />);
    const trigger = screen.getByTestId("user-menu-trigger");
    expect(trigger).toHaveTextContent("octocat");
    expect(trigger).toHaveTextContent("O");
  });

  it("opens the menu on click and signs out", () => {
    useAuthMock.mockReturnValue(authValue());
    render(<UserMenu />);

    fireEvent.click(screen.getByTestId("user-menu-trigger"));
    fireEvent.click(screen.getByTestId("user-menu-logout"));

    expect(logout).toHaveBeenCalledTimes(1);
  });

  it("hides sign-out in disabled (local) mode", () => {
    useAuthMock.mockReturnValue(authValue({ disabled: true }));
    render(<UserMenu />);

    fireEvent.click(screen.getByTestId("user-menu-trigger"));

    expect(screen.queryByTestId("user-menu-logout")).toBeNull();
    // The menu still opens to show the identity label.
    expect(screen.getByRole("menu")).toHaveTextContent("Signed in as");
  });
});
