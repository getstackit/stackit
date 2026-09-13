import type { StackDetail, TrunkCommitResponse } from "@/lib/api";

// Keep whole stacks intact so matching branches retain their parent context.
export function filterStacks(stacks: StackDetail[], query: string): StackDetail[] {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return stacks;

  return stacks.filter((stack) => {
    const fields = [
      stack.rootBranch,
      stack.title,
      stack.owner ? `@${stack.owner}` : undefined,
      stack.scope,
      ...stack.branches.flatMap((branch) => [
        branch.name,
        branch.pr?.title,
        branch.pr ? `#${branch.pr.number}` : undefined,
        branch.commitAuthor,
        branch.revision,
        ...branch.commits?.flatMap((commit) => [commit.message, commit.sha]) ?? [],
      ]),
    ].filter((field): field is string => Boolean(field));
    const searchable = fields.join(" ").toLowerCase();
    return terms.every((term) => searchable.includes(term));
  });
}

export function filterRecentCommits(commits: TrunkCommitResponse[], query: string): TrunkCommitResponse[] {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return commits;

  return commits.filter((commit) => {
    const searchable = [
      commit.message, commit.sha, commit.author, `@${commit.author}`,
      commit.prNumber ? `#${commit.prNumber}` : undefined,
      commit.stackScope,
      ...commit.stackPRs?.map((number) => `#${number}`) ?? [],
      ...Object.values(commit.stackPRTitles ?? {}),
    ].filter(Boolean).join(" ").toLowerCase();
    return terms.every((term) => searchable.includes(term));
  });
}
