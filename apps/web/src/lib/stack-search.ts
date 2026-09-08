import type { StackDetail } from "@/lib/api";

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
      ]),
    ].filter((field): field is string => Boolean(field));
    const searchable = fields.join(" ").toLowerCase();
    return terms.every((term) => searchable.includes(term));
  });
}
