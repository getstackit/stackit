"""Regression checks for benchmark timing boundaries and failure handling."""

import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

import benchmark


class MeasurementTests(unittest.TestCase):
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
