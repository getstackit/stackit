import type { NextConfig } from "next";
import { PHASE_PRODUCTION_BUILD } from "next/constants";

// `output: "export"` emits a static SPA that the Go server embeds and serves
// (with an index.html fallback for deep links). It is only correct for the
// production build: under `next dev` it forces every navigable path to be
// enumerated in generateStaticParams, which breaks client-side navigation to
// dynamic /{owner}/{repo}/... routes (the app routes entirely client-side via
// usePathname). So enable it only for `next build`; `next dev` runs as a normal
// dev server where the optional catch-all handles every path.
export default (phase: string): NextConfig => ({
  output: phase === PHASE_PRODUCTION_BUILD ? "export" : undefined,
});
