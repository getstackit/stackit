"use client";

import { useEffect, useRef, useState } from "react";
import { LogOut } from "lucide-react";
import { useAuth } from "@/components/providers/auth-provider";

// UserMenu renders the signed-in identity in the header as an avatar + login
// that opens a small popover. It pulls the user from auth context, so it
// renders nothing when there is no session (e.g. before auth resolves, or in
// tests without a provider). Sign-out is hidden in disabled/local mode where
// it would be a no-op and would wrongly flip the app into the login screen.
//
// Self-contained rather than built on a dropdown primitive — the app has no
// popover/dropdown UI primitive, and a single menu doesn't justify adding one.
export function UserMenu() {
  const { user, disabled, logout } = useAuth();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  if (!user) {
    return null;
  }

  const initial = user.login.charAt(0).toUpperCase();

  return (
    <div className="relative" ref={containerRef}>
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid="user-menu-trigger"
        className="flex items-center gap-2 rounded-md px-1 py-0.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="flex h-6 w-6 items-center justify-center rounded-full bg-foreground text-xs font-medium text-background">
          {initial}
        </span>
        <span className="max-w-[14ch] truncate">{user.login}</span>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 z-50 mt-2 w-48 overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
        >
          <div className="flex flex-col px-2 py-1.5">
            <span className="text-xs text-muted-foreground">Signed in as</span>
            <span className="truncate text-sm font-medium">{user.login}</span>
          </div>
          {!disabled && (
            <>
              <div className="my-1 h-px bg-border" />
              <button
                type="button"
                role="menuitem"
                data-testid="user-menu-logout"
                onClick={() => {
                  setOpen(false);
                  void logout();
                }}
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm transition-colors hover:bg-accent hover:text-accent-foreground"
              >
                <LogOut className="h-4 w-4" />
                Sign out
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
