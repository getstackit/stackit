"use client";

import { createContext, useContext, useEffect, useState } from "react";
import { fetchConfig, type ConfigResponse } from "@/lib/api";

const ConfigContext = createContext<ConfigResponse>({
  readOnly: false,
  authRequired: true,
});

export function useConfig() {
  return useContext(ConfigContext);
}

interface ConfigProviderProps {
  // fallback is used when the server doesn't answer /config (e.g. an older
  // build, or a network error). Pass a stable object so the effect doesn't
  // re-run every render.
  fallback: ConfigResponse;
  children: React.ReactNode;
}

// ConfigProvider fetches server capabilities once and exposes them via
// context. It blocks rendering until the value resolves (success or
// fallback) so descendants — notably AuthProvider — can rely on it being
// settled. The fetch is unauthenticated and cheap.
export function ConfigProvider({ fallback, children }: ConfigProviderProps) {
  const [config, setConfig] = useState<ConfigResponse | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchConfig()
      .then((c) => {
        if (!cancelled) setConfig(c);
      })
      .catch(() => {
        // Degrade gracefully: a server without /config (or an unreachable
        // one) falls back to the caller-supplied defaults rather than
        // blocking the app.
        if (!cancelled) setConfig(fallback);
      });
    return () => {
      cancelled = true;
    };
  }, [fallback]);

  if (config === null) {
    return (
      <div className="flex min-h-screen items-center justify-center p-8">
        <span className="text-sm text-muted-foreground">Loading…</span>
      </div>
    );
  }

  return <ConfigContext.Provider value={config}>{children}</ConfigContext.Provider>;
}
