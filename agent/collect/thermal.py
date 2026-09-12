"""Temperatures from hwmon: the processor, the memory modules, the NVMe disks."""
import os

HWMON = "/sys/class/hwmon"

# hwmon names a chip after its driver, not after the part it measures: the
# same processor reads as coretemp on Intel and k10temp on AMD, a DDR5 module
# as spd5118. Everything not listed — acpitz, the board's own zones, fans — is
# not a temperature the screen could put a name to, so it is left where it is.
CPU_CHIPS = {"coretemp", "k10temp", "zenpower", "cpu_thermal"}
MEM_CHIPS = {"spd5118", "jc42"}
DISK_CHIPS = {"nvme"}

# One reading stands for the whole processor. The package sensor comes before
# a core: the cooling is designed against the package, and a single core spikes
# on every scheduler tick. Tdie comes before Tctl: the latter carries an offset
# meant for the fan curve, not a temperature.
CPU_LABELS = ("Tdie", "Package id 0", "Tctl")
DISK_LABELS = ("Composite",)


def read_text(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            return f.read().strip()
    except OSError:
        return ""


def readings(chip):
    """Returns {label: celsius} for every input the chip answers right now.

    A sensor that is present but silent — a disk asleep, a bus error — fails
    on the read itself and is skipped rather than written down as zero.
    """
    out = {}
    for entry in sorted(os.listdir(chip)):
        if not (entry.startswith("temp") and entry.endswith("_input")):
            continue
        try:
            with open(os.path.join(chip, entry), encoding="ascii") as f:
                milli = int(f.read().strip())
        except (OSError, ValueError):
            continue
        stem = entry[:-len("_input")]
        label = read_text(os.path.join(chip, stem + "_label")) or stem
        out[label] = round(milli / 1000, 1)
    return out


def pick(temps, preferred):
    """The preferred reading of a chip, or its hottest one when none is labelled so."""
    for label in preferred:
        if label in temps:
            return temps[label]
    return max(temps.values()) if temps else None


def disk_of(chip, fallback):
    """The controller behind an nvme chip: its kernel name and the model string."""
    device = os.path.join(chip, "device")
    if not os.path.exists(device):
        return fallback, ""
    return os.path.basename(os.path.realpath(device)), read_text(os.path.join(device, "model"))


def temperatures(root=HWMON):
    """Returns the temperatures that have a sensor behind them.

    A key that is missing means there is no such sensor on this machine: the
    screen then says nothing, and the history keeps a null, not a zero.
    """
    try:
        chips = sorted(os.listdir(root))
    except OSError:
        return {}

    cpu, mem, disks = [], [], []
    for entry in chips:
        chip = os.path.join(root, entry)
        name = read_text(os.path.join(chip, "name"))
        if name in CPU_CHIPS:
            temp = pick(readings(chip), CPU_LABELS)
            if temp is not None:
                cpu.append(temp)
        elif name in MEM_CHIPS:
            temp = pick(readings(chip), ())
            if temp is not None:
                mem.append(temp)
        elif name in DISK_CHIPS:
            temp = pick(readings(chip), DISK_LABELS)
            if temp is not None:
                disk, model = disk_of(chip, entry)
                disks.append({"name": disk, "model": model, "temp": temp})

    out = {}
    if cpu:
        out["cpuTemp"] = max(cpu)
    if mem:
        out["memTemp"] = max(mem)
    if disks:
        out["diskTemps"] = sorted(disks, key=lambda d: d["name"])
        out["diskTemp"] = max(d["temp"] for d in disks)
    return out
