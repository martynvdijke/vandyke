#!/usr/bin/env python3
"""Build versioned zip archives for the vanDyke TRMNL plugins.

Each plugin is packaged with its directory as the archive root, which is what
``trmnlp`` expects when the archive is extracted. A combined archive containing
all four plugins and the TRMNL README is produced as well.

Usage:
    python3 scripts/package-trmnl.py 1.3.0
"""

from __future__ import annotations

import shutil
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PLUGIN_ROOT = ROOT / "trmnl" / "plugins"
README = ROOT / "trmnl" / "README.md"
OUTPUT_ROOT = ROOT / "dist" / "trmnl"


def _plugin_files(plugin: Path):
    for path in sorted(plugin.rglob("*")):
        if not path.is_file():
            continue
        if "__pycache__" in path.parts or path.suffix == ".pyc":
            continue
        yield path


def _add_plugin(archive: zipfile.ZipFile, plugin: Path) -> None:
    for path in _plugin_files(plugin):
        archive.write(path, (Path(plugin.name) / path.relative_to(plugin)).as_posix())


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {Path(sys.argv[0]).name} VERSION", file=sys.stderr)
        return 2

    version = sys.argv[1].strip().removeprefix("v")
    if not version:
        print("version must not be empty", file=sys.stderr)
        return 2

    plugins = sorted(path for path in PLUGIN_ROOT.iterdir() if path.is_dir())
    if not plugins:
        print(f"no plugins found in {PLUGIN_ROOT}", file=sys.stderr)
        return 1

    shutil.rmtree(OUTPUT_ROOT, ignore_errors=True)
    OUTPUT_ROOT.mkdir(parents=True, exist_ok=True)
    written: list[Path] = []

    for plugin in plugins:
        target = OUTPUT_ROOT / f"{plugin.name}-{version}.zip"
        with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            _add_plugin(archive, plugin)
        written.append(target)

    combined = OUTPUT_ROOT / f"vandyke-trmnl-plugins-{version}.zip"
    with zipfile.ZipFile(combined, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for plugin in plugins:
            _add_plugin(archive, plugin)
        if README.is_file():
            archive.write(README, "README.md")
    written.append(combined)

    for path in written:
        print(path.relative_to(ROOT))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
