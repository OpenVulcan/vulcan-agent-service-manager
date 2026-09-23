#!/usr/bin/env python3
"""Package and verify five independent vasm manager Release assets.
打包并校验五个平台的独立 vasm 管理器发布资产。
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import tarfile
import zipfile
from pathlib import Path


# TARGETS maps stable platform identifiers to their archive format and binary name.
# TARGETS 将稳定平台标识映射至归档格式和二进制文件名。
TARGETS = {
    "windows-x64": (".zip", "vasm.exe"),
    "linux-x64": (".tar.gz", "vasm"),
    "linux-arm64": (".tar.gz", "vasm"),
    "macos-x64": (".tar.gz", "vasm"),
    "macos-arm64": (".tar.gz", "vasm"),
}


# TAG_PATTERN accepts only release versions that are safe in asset names.
# TAG_PATTERN 仅接受可安全用于资产名称的发布版本。
TAG_PATTERN = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


# digest computes a streaming SHA-256 value for one Release artifact.
# digest 流式计算一份发布产物的 SHA-256 值。
# path identifies the artifact; the return value is a lowercase hex digest.
# path 指定产物；返回值为小写十六进制摘要。
def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


# archive_name returns the fixed package name for one platform.
# archive_name 返回某个平台的固定包名。
# platform is a TARGETS key; the return value is the asset filename.
# platform 是 TARGETS 的键；返回值是资产文件名。
def archive_name(platform: str) -> str:
    extension, _ = TARGETS[platform]
    return f"vasm-{platform}{extension}"


# package creates one native manager archive and its SHA-256 sidecar.
# package 创建一份原生管理器归档及其 SHA-256 校验文件。
# platform identifies the target, binary is its compiled executable, and output is the asset directory.
# platform 标识目标平台，binary 是编译后的可执行文件，output 是资产目录。
def package(platform: str, binary: Path, output: Path) -> None:
    if platform not in TARGETS:
        raise ValueError(f"unsupported platform: {platform}")
    extension, binary_name = TARGETS[platform]
    if not binary.is_file() or binary.name != binary_name:
        raise ValueError(f"expected native binary named {binary_name}")
    output.mkdir(parents=True, exist_ok=True)
    name = archive_name(platform)
    destination = output / name
    if destination.exists() or (output / f"{name}.sha256").exists():
        raise ValueError(f"release asset already exists: {name}")
    member = f"vasm-{platform}/{binary_name}"
    if extension == ".zip":
        with zipfile.ZipFile(destination, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
            entry = zipfile.ZipInfo(member)
            entry.external_attr = 0o755 << 16
            entry.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(entry, binary.read_bytes())
    else:
        with tarfile.open(destination, "x:gz") as archive:
            entry = tarfile.TarInfo(member)
            entry.size = binary.stat().st_size
            entry.mode = 0o755
            with binary.open("rb") as source:
                archive.addfile(entry, source)
    (output / f"{name}.sha256").write_text(f"{digest(destination)}  {name}\n", encoding="utf-8")


# verify checks exactly five native archives and writes an aggregate checksum manifest.
# verify 检查精确五份原生归档并写入总校验清单。
# output is the directory downloaded from build jobs, and tag is the published version.
# output 是从构建任务下载的目录，tag 是发布版本。
def verify(output: Path, tag: str) -> None:
    if not TAG_PATTERN.fullmatch(tag):
        raise ValueError("invalid release tag")
    expected = {archive_name(platform) for platform in TARGETS}
    expected_files = expected | {f"{name}.sha256" for name in expected}
    actual_files = {path.name for path in output.iterdir()} - {"SHA256SUMS", "release-index.json"}
    if actual_files != expected_files:
        raise ValueError(f"asset matrix mismatch: missing={sorted(expected_files-actual_files)}, extra={sorted(actual_files-expected_files)}")
    lines = []
    assets = []
    for platform in TARGETS:
        name = archive_name(platform)
        path = output / name
        sidecar = output / f"{name}.sha256"
        if not path.is_file() or path.is_symlink() or not sidecar.is_file() or sidecar.is_symlink():
            raise ValueError(f"release asset must be an ordinary file: {name}")
        expected_member = f"vasm-{platform}/{TARGETS[platform][1]}"
        if name.endswith(".zip"):
            with zipfile.ZipFile(path) as archive:
                if archive.namelist() != [expected_member]:
                    raise ValueError(f"unexpected archive layout: {name}")
        else:
            with tarfile.open(path, "r:gz") as archive:
                members = archive.getmembers()
                if len(members) != 1 or members[0].name != expected_member or not members[0].isfile() or not members[0].mode & 0o111:
                    raise ValueError(f"unexpected archive layout or executable mode: {name}")
        expected_line = f"{digest(path)}  {name}\n"
        if sidecar.read_text(encoding="utf-8") != expected_line:
            raise ValueError(f"checksum mismatch: {name}")
        lines.append(expected_line)
        assets.append({"platform": platform, "name": name, "size_bytes": path.stat().st_size, "sha256": digest(path)})
    (output / "SHA256SUMS").write_text("".join(lines), encoding="utf-8")
    index = {"schema_version": 1, "repository": "OpenVulcan/vulcan-agent-service-manager", "tag": tag, "assets": assets}
    (output / "release-index.json").write_text(json.dumps(index, indent=2) + "\n", encoding="utf-8")


# main parses the package or verify operation and reports validation errors.
# main 解析打包或校验操作，并报告验证错误。
# argv is the optional argument list; the return value is the process exit code.
# argv 是可选参数列表；返回值是进程退出码。
def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    package_parser = commands.add_parser("package")
    package_parser.add_argument("--platform", required=True)
    package_parser.add_argument("--binary", type=Path, required=True)
    package_parser.add_argument("--output", type=Path, required=True)
    verify_parser = commands.add_parser("verify")
    verify_parser.add_argument("--directory", type=Path, required=True)
    verify_parser.add_argument("--tag", required=True)
    args = parser.parse_args(argv)
    if args.command == "package":
        package(args.platform, args.binary, args.output)
    else:
        verify(args.directory, args.tag)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
