#!/usr/bin/env python3
"""Benchmark common local Stackit operations across git revisions.

The harness builds each requested revision, creates an equivalent fixture with
that binary, and measures commands against a clean copy of the fixture. This
keeps setup, cloning, compilation, hooks, and network traffic out of timings.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import platform
from pathlib import Path
import shutil
import statistics
import subprocess
import sys
import tarfile
import tempfile
import time
from dataclasses import dataclass


DEFAULT_REFS = ["v0.25.0", "v0.26.1", "v0.27.1", "main"]
COMMON_FLAGS = ["--no-interactive", "--no-verify"]


@dataclass(frozen=True)
class Case:
    name: str
    command: list[str]
    fresh_copy: bool = False


CASES = [
    Case("tree-short", ["tree", "short"]),
    Case("tree", ["tree"]),
    Case("info", ["info"]),
    Case("parent", ["parent"]),
    Case("children", ["children"]),
    Case("checkout-exact", ["co", "branch-01", "--quiet"], fresh_copy=True),
    Case("create", ["create", "benchmark-new", "-m", "perf: benchmark"], fresh_copy=True),
    Case("modify", ["modify", "-m", "perf: benchmark"], fresh_copy=True),
    Case("modify-midstack", ["modify", "-m", "perf: benchmark"], fresh_copy=True),
    Case("restack-noop", ["restack"], fresh_copy=True),
    Case("restack-upstack", ["restack", "--upstack"], fresh_copy=True),
]


def run(command: list[str], *, cwd: Path | None = None, capture: bool = False) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        cwd=cwd,
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )


def git(cwd: Path, *args: str) -> None:
    run(["git", *args], cwd=cwd)


def source_at(repo: Path, ref: str, destination: Path) -> None:
    archive = subprocess.Popen(["git", "archive", "--format=tar", ref], cwd=repo, stdout=subprocess.PIPE)
    assert archive.stdout is not None
    with tarfile.open(fileobj=archive.stdout, mode="r|") as tar:
        tar.extractall(destination, filter="data")
    if archive.wait() != 0:
        raise RuntimeError(f"git archive failed for {ref}")


def build(repo: Path, ref: str, directory: Path, go_binary: str, environment: dict[str, str]) -> tuple[Path, str]:
    source = directory / "source"
    source.mkdir()
    source_at(repo, ref, source)
    binary = directory / "stackit"
    completed = subprocess.run(
        [go_binary, "build", "-buildvcs=false", "-o", str(binary), "./apps/cli"],
        cwd=source,
        text=True,
        capture_output=True,
        env=environment,
    )
    if completed.returncode:
        raise RuntimeError(f"build failed for {ref}: {completed.stderr.strip() or completed.stdout.strip()}")
    commit = run(["git", "rev-parse", ref], cwd=repo, capture=True).stdout.strip()
    return binary, commit


def create_fixture(binary: Path, directory: Path, branches: int) -> Path:
    fixture = directory / "fixture"
    fixture.mkdir()
    git(fixture, "init", "--initial-branch=main")
    git(fixture, "config", "user.email", "benchmark@stackit.dev")
    git(fixture, "config", "user.name", "Stackit Benchmark")
    git(fixture, "config", "commit.gpgSign", "false")
    git(fixture, "config", "core.hooksPath", "/dev/null")
    git(fixture, "config", "gc.auto", "0")
    (fixture / "README.md").write_text("# Stackit benchmark fixture\n")
    git(fixture, "add", "README.md")
    git(fixture, "commit", "-m", "chore: fixture root")
    run([str(binary), *COMMON_FLAGS, "init"], cwd=fixture)
    for number in range(1, branches + 1):
        (fixture / f"branch-{number:02}.txt").write_text(f"fixture branch {number}\n")
        git(fixture, "add", ".")
        command = [str(binary), *COMMON_FLAGS, "create", f"branch-{number:02}", "-m", f"perf: branch {number}"]
        completed = subprocess.run(command, cwd=fixture, text=True, capture_output=True)
        if completed.returncode:
            raise RuntimeError(
                f"fixture creation failed for branch-{number:02}: "
                f"{completed.stderr.strip() or completed.stdout.strip()}"
            )
    return fixture


def percentile(values: list[float], percentile_: float) -> float:
    ordered = sorted(values)
    rank = (len(ordered) - 1) * percentile_ / 100
    low, high = math.floor(rank), math.ceil(rank)
    return ordered[low] + (ordered[high] - ordered[low]) * (rank - low)


def measure_case(binary: Path, fixture: Path, case: Case, runs: int, warmup: int, work: Path) -> dict[str, object]:
    durations: list[float] = []
    command = [str(binary), *COMMON_FLAGS, *case.command]

    for iteration in range(warmup + runs):
        trial = fixture
        if case.fresh_copy:
            trial = work / f"{case.name}-{iteration}"
            shutil.copytree(fixture, trial)
        try:
            if case.name in {"modify-midstack", "restack-upstack"}:
                git(trial, "checkout", "branch-01")
            if case.name in {"create", "modify", "modify-midstack", "restack-upstack"}:
                (trial / "benchmark-change.txt").write_text("benchmark change\n")
                git(trial, "add", "benchmark-change.txt")
            if case.name == "restack-upstack":
                git(trial, "commit", "--amend", "--no-edit")
            start = time.perf_counter_ns()
            run(command, cwd=trial)
            elapsed = (time.perf_counter_ns() - start) / 1_000_000
        except subprocess.CalledProcessError as error:
            raise RuntimeError(
                f"{case.name} failed: {error.stderr or error}"
            ) from error
        finally:
            if case.fresh_copy:
                shutil.rmtree(trial)
        if iteration >= warmup:
            durations.append(elapsed)

    return {
        "name": case.name,
        "command": case.command,
        "runs": runs,
        "median_ms": round(statistics.median(durations), 3),
        "mean_ms": round(statistics.mean(durations), 3),
        "p95_ms": round(percentile(durations, 95), 3),
        "min_ms": round(min(durations), 3),
        "max_ms": round(max(durations), 3),
        "samples_ms": [round(value, 3) for value in durations],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--refs", nargs="+", default=DEFAULT_REFS, help="Git refs to compare")
    parser.add_argument("--runs", type=int, default=10, help="Measured runs per case (default: 10)")
    parser.add_argument("--warmup", type=int, default=2, help="Warmup runs per case (default: 2)")
    parser.add_argument("--branches", type=int, default=10, help="Linear stack depth (default: 10)")
    parser.add_argument("--output", type=Path, default=Path("benchmark-results.json"), help="JSON result path")
    parser.add_argument(
        "--cache-dir",
        type=Path,
        default=Path("/tmp/stackit-benchmark-go-cache"),
        help="Writable Go build and module cache (default: /tmp/stackit-benchmark-go-cache)",
    )
    args = parser.parse_args()
    if args.runs < 1 or args.warmup < 0 or args.branches < 2:
        parser.error("--runs must be positive, --warmup non-negative, and --branches at least 2")

    repo = Path(__file__).resolve().parents[1]
    output = args.output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    cache_dir = args.cache_dir.resolve()
    cache_dir.mkdir(parents=True, exist_ok=True)
    # `go` is normally a mise shim. Archives intentionally are not trusted mise
    # projects, so resolve the real compiler before changing into one.
    go_binary = run(["mise", "which", "go"], cwd=repo, capture=True).stdout.strip()
    result: dict[str, object] = {
        "schema_version": 2,
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "platform": {
            "python": sys.version.split()[0],
            "git": run(["git", "--version"], capture=True).stdout.strip(),
            "go": run([go_binary, "version"], capture=True).stdout.strip(),
            "system": platform.platform(),
            "cpu_count": os.cpu_count(),
        },
        "fixture": {"shape": "linear", "branches": args.branches},
        "runs": args.runs,
        "warmup": args.warmup,
        "results": [],
    }

    with tempfile.TemporaryDirectory(prefix="stackit-benchmark-") as temporary:
        root = Path(temporary)
        # Keep user configuration, signing, hooks, and logging out of fixtures.
        os.environ.update({"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1",
                           "STACKIT_NO_LOGGING": "1", "GIT_TERMINAL_PROMPT": "0"})
        for key in list(os.environ):
            if key.startswith("GIT_CONFIG_KEY_") or key.startswith("GIT_CONFIG_VALUE_") or key in {
                "GIT_CONFIG_COUNT", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
            }:
                os.environ.pop(key)
        build_environment = os.environ | {
            "GOMODCACHE": str(cache_dir / "mod"),
            "GOCACHE": str(cache_dir / "build"),
        }
        for index, ref in enumerate(args.refs):
            print(f"benchmarking {ref}", flush=True)
            revision_dir = root / str(index)
            revision_dir.mkdir()
            binary, commit = build(repo, ref, revision_dir, go_binary, build_environment)
            fixture = create_fixture(binary, revision_dir, args.branches)
            work = revision_dir / "work"
            work.mkdir()
            cases = [measure_case(binary, fixture, case, args.runs, args.warmup, work) for case in CASES]
            result["results"].append({"ref": ref, "commit": commit, "cases": cases})  # type: ignore[index]
            output.write_text(json.dumps(result, indent=2) + "\n")

    output.write_text(json.dumps(result, indent=2) + "\n")
    print(f"wrote {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
