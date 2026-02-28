import { useEffect, useMemo, useState } from "react";

type RepoResponse = {
  owner: string;
  repo: string;
  trunk: string;
  currentBranch: string;
  remote: string;
  currentUser?: string;
};

type StackSummary = {
  rootBranch: string;
  title: string;
  status: string;
  scope?: string;
  branchCount: number;
  prCount: number;
  isCurrent: boolean;
};

type ViewResponse = {
  repo: RepoResponse;
  stacks: StackSummary[];
};

const VIEW_ENDPOINT = "/api/v1/view";
const EVENTS_ENDPOINT = "/api/v1/events";

export function App() {
  const [data, setData] = useState<ViewResponse | null>(null);
  const [error, setError] = useState<string>("");
  const [refreshCount, setRefreshCount] = useState(0);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const res = await fetch(VIEW_ENDPOINT);
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`);
        }
        const payload = (await res.json()) as ViewResponse;
        if (!cancelled) {
          setData(payload);
          setError("");
        }
      } catch (err) {
        if (!cancelled) {
          setError((err as Error).message);
        }
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [refreshCount]);

  useEffect(() => {
    const events = new EventSource(EVENTS_ENDPOINT);
    events.addEventListener("refresh", () => {
      setRefreshCount((count) => count + 1);
    });
    events.onerror = () => {
      // Leave page functional even if SSE is unavailable.
      events.close();
    };
    return () => {
      events.close();
    };
  }, []);

  const title = useMemo(() => {
    if (!data) {
      return "Stackit";
    }
    if (!data.repo.owner || !data.repo.repo) {
      return "Stackit";
    }
    return `${data.repo.owner}/${data.repo.repo}`;
  }, [data]);

  return (
    <main className="page">
      <header className="header">
        <h1>{title}</h1>
        <p>
          Trunk: <strong>{data?.repo.trunk ?? "..."}</strong> | Current:{" "}
          <strong>{data?.repo.currentBranch ?? "..."}</strong>
        </p>
      </header>
      {error ? <p className="error">Failed to load view: {error}</p> : null}
      <section className="panel">
        <h2>Stacks</h2>
        <ul className="stack-list">
          {(data?.stacks ?? []).map((stack) => (
            <li key={stack.rootBranch} className="stack-item">
              <div className="row">
                <strong>{stack.rootBranch}</strong>
                <span className={`badge badge-${stack.status}`}>{stack.status}</span>
              </div>
              <div className="meta">
                {stack.branchCount} branches · {stack.prCount} PRs
                {stack.scope ? ` · scope:${stack.scope}` : ""}
                {stack.isCurrent ? " · current" : ""}
              </div>
            </li>
          ))}
        </ul>
      </section>
    </main>
  );
}
