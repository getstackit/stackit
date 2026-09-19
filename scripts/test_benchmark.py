"""Regression checks for benchmark timing boundaries and failure handling."""

import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import benchmark


class MeasurementTests(unittest.TestCase):
    def test_absorb_stages_an_existing_downstack_file_before_timing(self):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary) / "fixture"
            fixture.mkdir()
            (fixture / "branch-01.txt").write_text("fixture branch 1\nadditional commit 2\n")
            events = []
            case = next(case for case in benchmark.CASES if case.name == "absorb-dry-run")

            def invoke(command, *, cwd):
                events.append(command[0])
                self.assertEqual((cwd / "branch-01.txt").read_text(), "fixture branch 1 (absorbed)\nadditional commit 2\n")

            def clock():
                events.append("clock")
                return 0

            with (
                patch.object(benchmark, "run", side_effect=invoke),
                patch.object(benchmark.time, "perf_counter_ns", side_effect=clock),
            ):
                benchmark.measure_case(Path("stackit"), fixture, case, runs=1, warmup=0, work=Path(temporary))
            self.assertEqual(events, ["git", "clock", "stackit", "clock"])
            self.assertEqual((fixture / "branch-01.txt").read_text(), "fixture branch 1\nadditional commit 2\n")

    def test_cleanup_is_outside_timer_and_warmup_is_excluded(self):
        events = []
        ticks = iter([0, 5_000_000, 10_000_000, 12_000_000])

        def clock():
            events.append("clock")
            return next(ticks)

        with (
            patch.object(benchmark.shutil, "copytree", side_effect=lambda *_: events.append("copy")),
            patch.object(benchmark.shutil, "rmtree", side_effect=lambda *_: events.append("cleanup")),
            patch.object(benchmark, "run", side_effect=lambda *_, **__: events.append("run")),
            patch.object(benchmark.time, "perf_counter_ns", side_effect=clock),
        ):
            result = benchmark.measure_case(
                Path("stackit"), Path("fixture"),
                benchmark.Case("checkout-exact", ["co", "branch-01"], fresh_copy=True),
                runs=1, warmup=1, work=Path("work"),
            )

        self.assertEqual(result["samples_ms"], [2.0])
        self.assertEqual(events, ["copy", "clock", "run", "clock", "cleanup"] * 2)

    def test_failed_mutation_is_not_retried(self):
        failure = subprocess.CalledProcessError(1, ["stackit"], stderr="original error")
        with (
            patch.object(benchmark.shutil, "copytree"),
            patch.object(benchmark.shutil, "rmtree") as cleanup,
            patch.object(benchmark, "run", side_effect=failure) as run,
        ):
            with self.assertRaisesRegex(RuntimeError, "original error"):
                benchmark.measure_case(
                    Path("stackit"), Path("fixture"),
                    benchmark.Case("checkout-exact", ["co", "branch-01"], fresh_copy=True),
                    runs=1, warmup=0, work=Path("work"),
                )
        run.assert_called_once()
        cleanup.assert_called_once()


if __name__ == "__main__":
    unittest.main()
