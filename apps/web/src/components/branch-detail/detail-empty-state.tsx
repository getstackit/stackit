import { GitBranch, GitPullRequest, Layers } from "lucide-react";
import type { StackDetail } from "@/lib/api";
import { stackStatusInfo } from "@/lib/status-config";

export function DetailEmptyState({ stacks }: { stacks: StackDetail[] }) {
  const branchCount = stacks.reduce((sum, stack) => sum + stack.branches.length, 0);
  const prCount = stacks.reduce(
    (sum, stack) => sum + stack.branches.filter((branch) => branch.pr).length,
    0
  );

  return (
    <div className="space-y-8 px-5 py-6">
      <section aria-label="Repository totals">
        <p className="mb-4 text-xs font-medium text-muted-foreground">Across this repository</p>
        <dl className="grid grid-cols-3 gap-3">
          {[
            { label: "Stacks", value: stacks.length, icon: Layers },
            { label: "Branches", value: branchCount, icon: GitBranch },
            { label: "Pull requests", value: prCount, icon: GitPullRequest },
          ].map(({ label, value, icon: Icon }) => (
            <div key={label}>
              <dt className="flex flex-col gap-3 text-xs text-muted-foreground">
                <Icon aria-hidden="true" className="size-4" />
                {label}
              </dt>
              <dd className="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
      </section>
      <section aria-labelledby="stack-health-heading">
        <h3 id="stack-health-heading" className="mb-3 text-xs font-medium text-muted-foreground">Stack status</h3>
        <dl className="divide-y rounded-lg border px-3">
          {Object.entries(stackStatusInfo).map(([status, info]) => (
            <div key={status} className="flex items-center justify-between gap-3 py-3 text-xs">
              <dt className="flex items-center gap-2" title={info.description}>
                <span aria-hidden="true" className={`size-1.5 rounded-full bg-current ${info.color}`} />
                {info.label}
              </dt>
              <dd className="font-medium tabular-nums">{stacks.filter((stack) => stack.status === status).length}</dd>
            </div>
          ))}
        </dl>
      </section>
      <div className="rounded-lg bg-muted/50 p-4">
        <h3 className="text-sm font-medium">Explore a stack</h3>
        <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
          Select a branch to review its changes, or a stack’s status to see its pull requests and checks.
        </p>
      </div>
    </div>
  );
}
