"use client";

import { useConfig } from "@/components/providers/config-provider";

// ReadOnlyBanner shows a thin notice when the server is in read-only mode,
// so viewers understand why write affordances (submit) are absent. It
// renders nothing on a writable server.
export function ReadOnlyBanner() {
  const { readOnly } = useConfig();
  if (!readOnly) {
    return null;
  }

  return (
    <div
      role="status"
      className="w-full bg-amber-100 px-4 py-1.5 text-center text-xs font-medium text-amber-900 dark:bg-amber-950 dark:text-amber-200"
    >
      Read-only view — changes are disabled on this server.
    </div>
  );
}
