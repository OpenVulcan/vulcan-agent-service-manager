#!/usr/bin/env python3
"""Run one natively built manager after extracting its Release archive.
解压并运行一份原生构建的管理器发布归档。
"""

from __future__ import annotations

import argparse
import subprocess
import tarfile
import tempfile
import zipfile
from pathlib import Path

from release import TARGETS, archive_name


# smoke extracts the selected native package and checks its reported version.
# smoke 解压所选原生包并检查其报告的版本。
# directory is the asset folder, platform is a TARGETS key, and version is the expected manager version.
# directory 是资产目录，platform 是 TARGETS 的键，version 是预期管理器版本。
def smoke(directory: Path, platform: str, version: str) -> None:
    if platform not in TARGETS:
        raise ValueError(f"unsupported platform: {platform}")
    extension, binary_name = TARGETS[platform]
    archive_path = directory / archive_name(platform)
    with tempfile.TemporaryDirectory() as temporary:
        extracted = Path(temporary)
        if extension == ".zip":
            with zipfile.ZipFile(archive_path) as package:
                package.extractall(extracted)
        else:
            with tarfile.open(archive_path, "r:gz") as package:
                package.extractall(extracted, filter="data")
        binary = extracted / f"vasm-{platform}" / binary_name
        completed = subprocess.run([str(binary), "--version"], check=True, capture_output=True, text=True, timeout=20)
        if completed.stdout.strip() != f"vasm {version}":
            raise ValueError(f"archive binary reported an unexpected version: {completed.stdout!r}")


# main parses the native archive path and exits after the smoke check.
# main 解析原生归档路径，并在冒烟检查后退出。
# argv is an optional argument list; the function returns a process exit code.
# argv 是可选参数列表；函数返回进程退出码。
def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--platform", required=True)
    parser.add_argument("--version", required=True)
    args = parser.parse_args(argv)
    smoke(args.directory, args.platform, args.version)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
