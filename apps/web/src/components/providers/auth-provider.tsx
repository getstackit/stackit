"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import {
  authLoginURL,
  fetchMe,
  logout as apiLogout,
  type MeResponse,
  UnauthorizedError,
} from "@/lib/api";
import { useConfig } from "@/components/providers/config-provider";

interface AuthContextValue {
  // user is null while loading; remains null after a 401 (the provider will
  // render the login screen instead of children in that case).
  user: MeResponse | null;
  isLoading: boolean;
  loginURL: string;
  logout: () => Promise<void>;
  // disabled mirrors the AuthProvider `disable` prop: true in local/dev mode
  // where there is no real session. Consumers use it to hide sign-out, which
  // would be a no-op (and would wrongly flip the app into the login screen).
  disabled: boolean;
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  isLoading: true,
  loginURL: "/auth/login",
  logout: async () => {},
  disabled: false,
});

export function useAuth() {
  return useContext(AuthContext);
}

interface AuthProviderProps {
  children: React.ReactNode;
}

const PLACEHOLDER_LOCAL_USER: MeResponse = { login: "local", id: 0 };

// AuthProvider gates the rest of the app on a /auth/me check. While the
// fetch is pending it renders nothing; on 401 it swaps in a sign-in card;
// on success it renders children with the user identity exposed via
// context. Network failures other than 401 render a small error message
// so the app doesn't silently hang on a broken backend.
//
// When the server reports that auth isn't required (a public read-only
// server, or auth-disabled), the /auth/me check is skipped and a
// placeholder identity is used — the same behavior the old build-time
// NEXT_PUBLIC_STACKIT_AUTH_DISABLED flag produced, now driven by runtime
// config so one build serves both private and public deployments.
export function AuthProvider({ children }: AuthProviderProps) {
  const { authRequired } = useConfig();
  const disable = !authRequired;

  const [user, setUser] = useState<MeResponse | null>(() =>
    disable ? PLACEHOLDER_LOCAL_USER : null
  );
  const [isLoading, setIsLoading] = useState(!disable);
  const [error, setError] = useState<string | null>(null);
  const [needsLogin, setNeedsLogin] = useState(false);

  // Fetch the current user on mount. The async work lives inside the effect
  // (not a synchronously-called callback) so all setState happens after an
  // await — the pattern the set-state-in-effect lint rule expects. The
  // `active` guard drops results that resolve after unmount.
  useEffect(() => {
    if (disable) {
      return;
    }
    let active = true;
    (async () => {
      try {
        const me = await fetchMe();
        if (!active) return;
        setUser(me);
        setNeedsLogin(false);
        setError(null);
      } catch (e) {
        if (!active) return;
        if (e instanceof UnauthorizedError) {
          setUser(null);
          setNeedsLogin(true);
        } else {
          setError(e instanceof Error ? e.message : String(e));
        }
      } finally {
        if (active) setIsLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, [disable]);

  const logout = useCallback(async () => {
    try {
      await apiLogout();
    } finally {
      setUser(null);
      setNeedsLogin(true);
    }
  }, []);

  const returnTo =
    typeof window !== "undefined" ? window.location.pathname + window.location.search : "/";
  const loginURL = authLoginURL(returnTo);

  if (isLoading) {
    return <AuthBoundary>Loading…</AuthBoundary>;
  }
  if (error) {
    return (
      <AuthBoundary>
        <p className="text-sm text-muted-foreground">
          Couldn&apos;t reach the API: {error}
        </p>
      </AuthBoundary>
    );
  }
  if (needsLogin) {
    return <SignIn loginURL={loginURL} />;
  }

  return (
    <AuthContext.Provider
      value={{ user, isLoading: false, loginURL, logout, disabled: Boolean(disable) }}
    >
      {children}
    </AuthContext.Provider>
  );
}

function AuthBoundary({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center p-8">
      <div className="max-w-md text-center">{children}</div>
    </div>
  );
}

function SignIn({ loginURL }: { loginURL: string }) {
  return (
    <AuthBoundary>
      <h1 className="mb-2 text-2xl font-semibold">stackit</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        Sign in with GitHub to continue.
      </p>
      <a
        href={loginURL}
        className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background hover:opacity-90"
      >
        Sign in with GitHub
      </a>
    </AuthBoundary>
  );
}
