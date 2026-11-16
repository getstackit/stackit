// TypeScript 7 (the native compiler) does not ship the TS 6 JavaScript API
// that typescript-eslint consumes. The app keeps `tsc` on TypeScript 7; the
// linter's packages get the official TS 6 compatibility package as a private
// dependency instead of resolving their `typescript` peer to the app's TS 7.
// pnpm `overrides` do not apply to peer dependencies, hence the hook.
//
// Remove this file once typescript-eslint supports TypeScript 7.
const TS6 = "npm:@typescript/typescript6@^6.0.2";

const NEEDS_TS6_API = new Set([
  "typescript-eslint",
  "ts-api-utils",
]);

function needsTs6(name) {
  return NEEDS_TS6_API.has(name) || name.startsWith("@typescript-eslint/");
}

function readPackage(pkg) {
  if (!needsTs6(pkg.name)) return pkg;
  const hasPeer = pkg.peerDependencies && pkg.peerDependencies.typescript;
  const hasDep = pkg.dependencies && pkg.dependencies.typescript;
  if (!hasPeer && !hasDep) return pkg;
  if (hasPeer) delete pkg.peerDependencies.typescript;
  if (pkg.peerDependenciesMeta) delete pkg.peerDependenciesMeta.typescript;
  pkg.dependencies = { ...pkg.dependencies, typescript: TS6 };
  return pkg;
}

module.exports = { hooks: { readPackage } };
