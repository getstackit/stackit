import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
  // eslint-plugin-react uses context.getFilename() which was removed in ESLint 10.
  // Setting an explicit version bypasses the broken auto-detection code path.
  {
    settings: {
      react: {
        version: "19.2.7",
      },
    },
  },
  // react-hooks/set-state-in-effect (introduced in react-hooks v7) flags async
  // data-fetching effects that call setState asynchronously — a valid and common
  // React pattern. The rule targets synchronous derived-state anti-patterns but
  // the static analysis also catches intentional async fetch-then-setState flows.
  {
    rules: {
      "react-hooks/set-state-in-effect": "off",
    },
  },
]);

export default eslintConfig;
