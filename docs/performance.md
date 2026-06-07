# Performance

Most stackit commands are fast and local. When a command feels slow, the cost is almost
always **network round-trips to GitHub** — the `git fetch` over SSH, plus any GitHub API
calls. This guide covers the biggest levers and how to diagnose a slow run.

## Reuse the SSH connection (biggest lever on slow links)

Every `git fetch` stackit runs opens a fresh SSH connection to GitHub, and the SSH
handshake is several round-trips. On a fast network that's ~0.5s; on a high-latency link
(VPN, far from GitHub's servers, flaky Wi-Fi) it can be several seconds — paid on *every*
fetch, by every stackit command that touches the remote.

Enabling SSH connection multiplexing keeps one connection alive and reuses it, eliminating
the per-fetch handshake. Add this to `~/.ssh/config`:

```sshconfig
Host github.com
    ControlMaster auto
    ControlPath ~/.ssh/control-%r@%h:%p
    ControlPersist 5m
```

The first connection in a 5-minute window pays the handshake; subsequent fetches reuse it.
This speeds up `get`, `sync`, `submit`, and every other remote operation — not just one
command.

## Why `get` is fast

`stackit get` discovers a branch's stack (its parent chain) from the **metadata** that the
fetch already brings down (`refs/stackit/metadata/*`), rather than making a serial GitHub
API call per ancestor. For a branch submitted via stackit, this means:

- A single branch whose parent is trunk: one fetch, zero GitHub calls.
- A deeper stack: two fetches total (target + metadata, then ancestor heads) regardless of
  depth.

stackit only falls back to a GitHub lookup when a branch has no stackit metadata (for
example a branch that was never submitted via stackit), and even then PR-number lookups
run in parallel rather than blocking the fetch.

## Diagnosing a slow command

stackit traces every git operation with a microsecond duration. Enable debug-level logging
to a file, run the slow command, then read the trace:

```bash
STACKIT_LOG_LEVEL=debug STACKIT_LOG_FILE=/tmp/st-trace.log stackit get <branch>
```

Each git operation is logged as an `[st-trace]` line with `op=` (operation) and `dur_us=`
(microseconds). To see which operation dominates:

```bash
python3 -c '
import re
rows = []
for line in open("/tmp/st-trace.log"):
    if "st-trace" not in line: continue
    o = re.search(r"op=(\S+)", line); d = re.search(r"dur_us=(\d+)", line)
    if o and d: rows.append((int(d.group(1)), o.group(1)))
rows.sort(reverse=True)
print(f"git ops: {len(rows)}, summed git time: {sum(d for d,_ in rows)/1e6:.2f}s")
[print(f"  {d/1000:8.1f} ms  {o}") for d, o in rows[:15]]
'
```

Interpreting the result:

- **`FetchRefSpecs` dominates** → the cost is the `git fetch` / SSH handshake. Enable SSH
  connection reuse (above).
- **A large gap between the summed git time and the wall-clock time** → the remaining time
  is GitHub API latency or shell `precmd` hooks (e.g. `mise`, `direnv`) firing around the
  command in your interactive shell.

> Note: logging is disabled when `STACKIT_NO_LOGGING` is set. Unset it to capture traces.
