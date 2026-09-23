package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestExtractRejectsTraversalAndDuplicates rejects names that could escape or overwrite files.
// TestExtractRejectsTraversalAndDuplicates 拒绝可能越界或覆盖文件的归档名称。
func TestExtractRejectsTraversalAndDuplicates(t *testing.T) {
	for name, entries := range map[string][]string{
		"parent":    {"vasm-windows-x64/../outside.txt"},
		"absolute":  {"/vasm-windows-x64/inside.txt"},
		"duplicate": {"vasm-windows-x64/vasm.exe", "vasm-windows-x64/VASM.EXE"},
	} {
		t.Run(name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "unsafe.zip")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			for _, entryName := range entries {
				entry, err := writer.Create(entryName)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte("payload")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if err := Extract(archivePath, filepath.Join(t.TempDir(), "stage"), "vasm-windows-x64"); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
		})
	}
}

// TestTrackerRejectsCumulativeOverflow ensures archive size accounting cannot wrap around.
// TestTrackerRejectsCumulativeOverflow 确保归档累计大小计数不能溢出绕回。
func TestTrackerRejectsCumulativeOverflow(t *testing.T) {
	tracker := newTracker()
	if _, err := tracker.target("root/first", t.TempDir(), "root", MaxExpandedBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.target("root/second", t.TempDir(), "root", 1); err == nil {
		t.Fatal("archive exceeded the expanded byte limit")
	}
}

// TestExtractTarLinks accepts package-contained links and rejects escaping or dangling targets.
// TestExtractTarLinks 接受包内链接，并拒绝逃逸或悬空的链接目标。
func TestExtractTarLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows requires an optional symlink creation privilege; Unix release runners exercise TAR links")
	}
	for _, testCase := range []struct {
		// name identifies the distinct link security case.
		// name 标识不同的链接安全用例。
		name string
		// target is the relative target stored in the TAR header.
		// target 是 TAR 头中保存的相对目标。
		target string
		// accepted states whether extraction must succeed.
		// accepted 表示解压是否必须成功。
		accepted bool
	}{
		{name: "contained", target: "../lib/corepack.js", accepted: true},
		{name: "escape", target: "../../outside", accepted: false},
		{name: "absolute", target: "/tmp/outside", accepted: false},
		{name: "dangling", target: "../lib/missing.js", accepted: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "package.tar.gz")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			compressor := gzip.NewWriter(file)
			writer := tar.NewWriter(compressor)
			payload := []byte("corepack")
			if err := writer.WriteHeader(&tar.Header{Name: "app/bin", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteHeader(&tar.Header{Name: "app/lib/corepack.js", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteHeader(&tar.Header{Name: "app/bin/corepack", Linkname: testCase.target, Mode: 0o777, Typeflag: tar.TypeSymlink}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compressor.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(t.TempDir(), "stage")
			err = Extract(archivePath, destination, "app")
			if (err == nil) != testCase.accepted {
				t.Fatalf("extract error=%v, accepted=%v", err, testCase.accepted)
			}
			if testCase.accepted {
				contents, err := os.ReadFile(filepath.Join(destination, "bin", "corepack"))
				if err != nil || string(contents) != string(payload) {
					t.Fatalf("linked content=%q, error=%v", contents, err)
				}
			}
		})
	}
}

// TestValidateTarLink rejects absolute, escaping, and Windows-style link targets on every platform.
// TestValidateTarLink 在所有平台上拒绝绝对、逃逸及 Windows 风格的链接目标。
func TestValidateTarLink(t *testing.T) {
	for _, target := range []string{"../../outside", "/tmp/outside", "..\\outside", ""} {
		if err := validateTarLink("app/bin/tool", target, "app"); err == nil {
			t.Fatalf("unsafe link target %q was accepted", target)
		}
	}
	if err := validateTarLink("app/bin/tool", "../lib/tool", "app"); err != nil {
		t.Fatal(err)
	}
}
