// Parsing and building for the GitHub-style browser URLs:
//
//   /{owner}/{repo}                     repo home (no selection)
//   /{owner}/{repo}/tree/{branch}       a branch (branch may contain slashes)
//   /{owner}/{repo}/stack/{rootBranch}  a whole stack
//   /{owner}/{repo}/pull/{number}       a PR, resolved client-side to its branch
//
// Branch and stack names can contain slashes, so they occupy every segment
// after the section keyword and are encoded per-segment (slashes stay as path
// separators; other special characters are percent-encoded).

export type Selection =
  | { type: "branch"; name: string }
  | { type: "stack"; rootBranch: string }
  | { type: "pull"; number: number };

export interface ParsedRoute {
  owner: string | null;
  repo: string | null;
  selection: Selection | null;
}

// encodeName encodes a branch/stack name for a URL path, preserving its
// slashes as separators so `feat/login` becomes `feat/login`, not `feat%2Flogin`.
function encodeName(name: string): string {
  return name.split("/").map(encodeURIComponent).join("/");
}

// decodeName reverses encodeName for a list of path segments.
function decodeName(segments: string[]): string {
  return segments.map(decodeURIComponent).join("/");
}

export function parseRepoPath(pathname: string): ParsedRoute {
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length < 2) {
    return { owner: null, repo: null, selection: null };
  }

  const owner = decodeURIComponent(segments[0]);
  const repo = decodeURIComponent(segments[1]);
  const rest = segments.slice(2);

  let selection: Selection | null = null;
  if (rest.length >= 2) {
    const [section, ...tail] = rest;
    switch (section) {
      case "tree":
        selection = { type: "branch", name: decodeName(tail) };
        break;
      case "stack":
        selection = { type: "stack", rootBranch: decodeName(tail) };
        break;
      case "pull": {
        const number = Number(tail[0]);
        if (Number.isInteger(number)) {
          selection = { type: "pull", number };
        }
        break;
      }
    }
  }

  return { owner, repo, selection };
}

export function buildRepoPath(
  owner: string,
  repo: string,
  selection: Selection | null
): string {
  const base = `/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
  if (!selection) return base;
  switch (selection.type) {
    case "branch":
      return `${base}/tree/${encodeName(selection.name)}`;
    case "stack":
      return `${base}/stack/${encodeName(selection.rootBranch)}`;
    case "pull":
      return `${base}/pull/${selection.number}`;
  }
}
