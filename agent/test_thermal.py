import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402
import agent  # noqa: E402,F401
from collect import thermal  # noqa: E402


class Hwmon(unittest.TestCase):
    """A hwmon tree built by hand: no sensor of this machine is read."""

    def setUp(self):
        self.root = test_barrier.tmp_path(prefix="aacpanel-hwmon-")
        self.addCleanup(shutil.rmtree, self.root, True)
        self.n = 0

    def chip(self, name, temps, device=None, model=None):
        """temps: {"temp1": (label or None, text of the input)}."""
        path = os.path.join(self.root, f"hwmon{self.n}")
        self.n += 1
        os.makedirs(path)
        with open(os.path.join(path, "name"), "w") as f:
            f.write(name + "\n")
        for stem, (label, raw) in temps.items():
            with open(os.path.join(path, stem + "_input"), "w") as f:
                f.write(raw)
            if label is not None:
                with open(os.path.join(path, stem + "_label"), "w") as f:
                    f.write(label + "\n")
        if device:
            target = os.path.join(self.root, "devices", device)
            os.makedirs(target, exist_ok=True)
            if model is not None:
                with open(os.path.join(target, "model"), "w") as f:
                    f.write(model + " \n")
            os.symlink(target, os.path.join(path, "device"))
        return path

    def test_the_processor_reports_its_package_not_its_hottest_core(self):
        self.chip("coretemp", {
            "temp1": ("Package id 0", "41000\n"),
            "temp2": ("Core 0", "38000\n"),
            "temp3": ("Core 1", "45000\n"),
        })
        self.assertEqual(thermal.temperatures(self.root), {"cpuTemp": 41.0})

    def test_an_amd_chip_prefers_the_die_over_the_control_value(self):
        self.chip("k10temp", {"temp1": ("Tctl", "52000\n"), "temp2": ("Tdie", "42000\n")})
        self.assertEqual(thermal.temperatures(self.root)["cpuTemp"], 42.0)

    def test_a_chip_without_labels_gives_its_hottest_reading(self):
        self.chip("cpu_thermal", {"temp1": (None, "55250\n"), "temp2": (None, "48000\n")})
        self.assertEqual(thermal.temperatures(self.root)["cpuTemp"], 55.2)

    def test_two_sockets_report_the_hotter_one(self):
        self.chip("coretemp", {"temp1": ("Package id 0", "41000\n")})
        self.chip("coretemp", {"temp1": ("Package id 1", "47000\n")})
        self.assertEqual(thermal.temperatures(self.root)["cpuTemp"], 47.0)

    def test_memory_is_the_hottest_module(self):
        self.chip("spd5118", {"temp1": (None, "35500\n")})
        self.chip("spd5118", {"temp1": (None, "36750\n")})
        self.assertEqual(thermal.temperatures(self.root), {"memTemp": 36.8})

    def test_a_machine_without_a_memory_sensor_has_no_memory_key(self):
        self.chip("coretemp", {"temp1": ("Package id 0", "41000\n")})
        out = thermal.temperatures(self.root)
        self.assertNotIn("memTemp", out)
        self.assertNotIn("diskTemps", out)
        self.assertNotIn("diskTemp", out)

    def test_disks_report_the_composite_by_controller_with_the_model(self):
        self.chip("nvme", {"temp1": ("Composite", "41800\n"), "temp2": ("Sensor 1", "58800\n")},
                  device="nvme0", model="Model A 1TB")
        self.chip("nvme", {"temp1": ("Composite", "35800\n")}, device="nvme1", model="Model B 512GB")
        out = thermal.temperatures(self.root)
        self.assertEqual(out["diskTemps"], [
            {"name": "nvme0", "model": "Model A 1TB", "temp": 41.8},
            {"name": "nvme1", "model": "Model B 512GB", "temp": 35.8},
        ])
        self.assertEqual(out["diskTemp"], 41.8)

    def test_a_disk_chip_without_a_device_link_keeps_its_hwmon_name(self):
        self.chip("nvme", {"temp1": ("Composite", "40000\n")})
        self.assertEqual(thermal.temperatures(self.root)["diskTemps"],
                         [{"name": "hwmon0", "model": "", "temp": 40.0}])

    def test_a_zone_that_measures_no_named_part_is_ignored(self):
        self.chip("acpitz", {"temp1": (None, "27800\n")})
        self.chip("acpi_fan", {})
        self.assertEqual(thermal.temperatures(self.root), {})

    def test_a_silent_input_is_skipped_not_written_as_zero(self):
        self.chip("coretemp", {"temp1": ("Package id 0", ""), "temp2": ("Core 0", "38000\n")})
        self.assertEqual(thermal.temperatures(self.root)["cpuTemp"], 38.0)

    def test_a_chip_whose_every_input_is_silent_counts_as_absent(self):
        self.chip("spd5118", {"temp1": (None, "")})
        self.assertEqual(thermal.temperatures(self.root), {})

    def test_no_hwmon_directory_means_no_temperatures(self):
        self.assertEqual(thermal.temperatures(os.path.join(self.root, "missing")), {})


if __name__ == "__main__":
    unittest.main()
