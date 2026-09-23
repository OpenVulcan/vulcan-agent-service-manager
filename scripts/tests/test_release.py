"""Validate the independent manager release archive and checksum contract.
验证独立管理器发布归档与校验清单契约。
"""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from scripts.release import TARGETS, package, verify


# ReleasePackagingTests exercises the exact five-asset release matrix.
# ReleasePackagingTests 验证精确的五资产发布矩阵。
class ReleasePackagingTests(unittest.TestCase):
    # test_all_targets_and_tamper_detection validates package assembly and checksum rejection.
    # test_all_targets_and_tamper_detection 验证打包组合与篡改校验拒绝。
    # self is the test case instance; the method reports assertion failures through unittest.
    # self 是测试实例；方法通过 unittest 报告断言失败。
    def test_all_targets_and_tamper_detection(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / "dist"
            for platform, (_, binary_name) in TARGETS.items():
                binary = root / platform / binary_name
                binary.parent.mkdir()
                binary.write_bytes(f"fake native binary: {platform}".encode())
                binary.chmod(0o755)
                package(platform, binary, output)
            verify(output, "v0.1.0")
            self.assertEqual(len(list(output.glob("*.sha256"))), 5)
            self.assertTrue((output / "SHA256SUMS").is_file())
            self.assertTrue((output / "release-index.json").is_file())
            archive = output / "vasm-windows-x64.zip"
            archive.write_bytes(archive.read_bytes() + b"tampered")
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                verify(output, "v0.1.0")


if __name__ == "__main__":
    unittest.main()
