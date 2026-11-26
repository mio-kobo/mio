from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import native_platforms


class DarwinBenchmarkParallelismTests(unittest.TestCase):
    def test_phase_groups_partition_all_phases_without_loss_or_overlap(self):
        all_phases = native_platforms.darwin_benchmark_phases_for_group("all")
        g1 = native_platforms.darwin_benchmark_phases_for_group("1")
        g2 = native_platforms.darwin_benchmark_phases_for_group("2")
        self.assertEqual(len(g1) + len(g2), len(all_phases))
        self.assertEqual(set(g1) & set(g2), set())
        self.assertEqual(set(g1) | set(g2), set(all_phases))

    def test_heavy_workloads_are_isolated_in_group_2(self):
        g2 = native_platforms.darwin_benchmark_phases_for_group("2")
        self.assertIn("alloc_4", g2)
        self.assertIn("mpmc_long", g2)

    def test_invalid_group_raises(self):
        with self.assertRaises(ValueError):
            native_platforms.darwin_benchmark_phases_for_group("3")


if __name__ == "__main__":
    unittest.main()
