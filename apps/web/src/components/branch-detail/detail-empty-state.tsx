import { GitBranch } from "lucide-react";

export function DetailEmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center px-8 py-10 text-center">
      <div className="mb-4 rounded-2xl border bg-muted/40 p-4 text-muted-foreground">
        <GitBranch aria-hidden="true" className="size-6" />
      </div>
      <h3 className="text-sm font-medium">Explore a stack</h3>
      <p className="mt-2 max-w-60 text-sm leading-relaxed text-muted-foreground">
        Select a branch to review its changes, or a stack to see its pull requests and checks.
      </p>
    </div>
  );
}
