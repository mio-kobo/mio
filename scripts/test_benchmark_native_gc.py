"""Validate measurement math and reject incomplete/corrupt telemetry."""
import importlib.util
from pathlib import Path
import unittest
import tempfile
import platform

spec = importlib.util.spec_from_file_location("native_gc", Path(__file__).with_name("benchmark-native-gc.py"))
bench = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bench)


class MeasurementTests(unittest.TestCase):
    def metrics(self):
        values = dict(gc_count=3, gc_last_ticks=5, gc_total_ticks=10,
                      gc_max_ticks=5, timer_frequency_hz=1000, runtime_ticks=100,
                      allocated_bytes=256, allocated_objects=2, heap_capacity=1024,
                      heap_live_bytes=64, heap_peak_live_bytes=128, heap_cursor=256)
        values.update({f"gc_pause_bucket_{i}": 0 for i in range(64)})
        # Samples 1, 4, 5 ticks -> inclusive bounds 1, 4, 8 ticks.
        for i in (0, 2, 3):
            values[f"gc_pause_bucket_{i}"] = 1
        return values

    def encode(self, values):
        return "".join(f"mio-gc {key}={value}\n" for key, value in values.items()).encode()

    def test_nearest_rank_and_empty(self):
        self.assertIsNone(bench.percentile([], .99))
        self.assertEqual(bench.percentile([10, 1, 2], .5), 2)
        self.assertEqual(bench.percentile([10, 1, 2], .99), 10)

    def test_histogram_upper_bounds_and_overflow(self):
        hist = [0] * 64
        self.assertIsNone(bench.histogram_percentile_upper(hist, .99, 1000))
        hist[0], hist[2], hist[3] = 1, 1, 1
        self.assertEqual(bench.histogram_percentile_upper(hist, .5, 1000), .004)
        self.assertEqual(bench.histogram_percentile_upper(hist, .99, 1000), .008)
        hist[63] = 1
        self.assertIsNone(bench.histogram_percentile_upper(hist, .99, 1000))

    def test_parse_and_validate(self):
        values = self.metrics()
        self.assertEqual(bench.parse_metrics(self.encode(values)), values)
        self.assertEqual(bench.parse_metrics(b""), {})
        for key, bad_value in (("gc_count", 4), ("timer_frequency_hz", 0),
                               ("gc_total_ticks", 101), ("heap_live_bytes", 129),
                               ("heap_peak_live_bytes", 1025), ("heap_cursor", 1025),
                               ("gc_max_ticks", 9)):
            with self.subTest(key=key), self.assertRaises(ValueError):
                bench.parse_metrics(self.encode({**values, key: bad_value}))
        with self.assertRaises(ValueError):
            bench.parse_metrics(self.encode(values) + b"mio-gc gc_count=3\n")
        del values["gc_pause_bucket_63"]
        with self.assertRaises(ValueError):
            bench.parse_metrics(self.encode(values))

    def test_generational_accounting(self):
        values = self.metrics()
        values.update(minor_gc_count=2, major_gc_count=1,
                      minor_gc_total_ticks=5, major_gc_total_ticks=5,
                      minor_gc_max_ticks=4, major_gc_max_ticks=5,
                      young_live_bytes=32, old_live_bytes=32,
                      promotion_threshold=3, promoted_objects=1,
                      promoted_bytes=32, remembered_set_entries=1)
        for kind in ("minor", "major"):
            values.update({f"{kind}_gc_pause_bucket_{i}": 0 for i in range(64)})
        values["minor_gc_pause_bucket_0"] = 1
        values["minor_gc_pause_bucket_2"] = 1
        values["major_gc_pause_bucket_3"] = 1
        self.assertEqual(bench.parse_metrics(self.encode(values)), values)
        sample = dict(wall_seconds=.2, peak_rss_bytes=4096,
                      user_cpu_seconds=.1, system_cpu_seconds=.01, metrics=values)
        summary = bench.summarize([sample])["gc"]
        self.assertEqual(summary["kind"], "non_moving_generational_stw")
        self.assertEqual(summary["minor"]["count"], 2)
        self.assertEqual(summary["major"]["pause_max_seconds"], .005)
        for key, value in (("minor_gc_count", 3), ("major_gc_total_ticks", 4),
                           ("young_live_bytes", 33), ("promotion_threshold", 9),
                           ("minor_gc_pause_bucket_3", 1), ("major_gc_max_ticks", 4)):
            with self.subTest(key=key), self.assertRaises(ValueError):
                bench.parse_metrics(self.encode({**values, key: value}))
        for key in ("major_gc_pause_bucket_63", "minor_gc_count", "young_live_bytes"):
            incomplete = dict(values)
            del incomplete[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                bench.parse_metrics(self.encode(incomplete))

    def test_los_is_outside_ordinary_cursor(self):
        values = self.metrics()
        values.update(heap_cursor=16, los_live_bytes=48)
        self.assertEqual(bench.parse_metrics(self.encode(values)), values)
        for changes in ({"los_live_bytes":65}, {"heap_cursor":15}, {"heap_peak_live_bytes":1025}):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                bench.parse_metrics(self.encode({**values, **changes}))

    @unittest.skipUnless(platform.system() == "Darwin", "Darwin RSS units")
    def test_child_resource_measurement_and_timeout(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            image = root / "child"
            image.write_text("#!/bin/sh\nprintf 'ok\\n'\n")
            image.chmod(0o700)
            entry = dict(path=str(image), expected_stdout="ok\n", observation=False, name="compute")
            sample = bench.run_sample(entry, root, "success", 2)
            self.assertEqual(sample["exit_code"], 0)
            self.assertGreater(sample["peak_rss_bytes"], 0)
            self.assertGreaterEqual(sample["user_cpu_seconds"], 0)
            image.write_text("#!/bin/sh\n/bin/sleep 10\n")
            with self.assertRaisesRegex(RuntimeError, "timeout=True"):
                bench.run_sample(entry, root, "timeout", .03)

    def test_summary_uses_all_collections(self):
        values = self.metrics()
        sample = dict(wall_seconds=.2, peak_rss_bytes=4096,
                      user_cpu_seconds=.1, system_cpu_seconds=.01, metrics=values)
        summary = bench.summarize([sample, sample])
        self.assertEqual(summary["gc"]["count"], 6)
        self.assertEqual(summary["gc"]["pause_p99_upper_seconds"], .008)
        self.assertEqual(summary["gc"]["gc_wall_fraction"], .1)
        self.assertEqual(summary["max_peak_rss_bytes"], 4096)
        self.assertIsNone(summary["gc"]["gc_cpu_percent"])


if __name__ == "__main__":
    unittest.main()
