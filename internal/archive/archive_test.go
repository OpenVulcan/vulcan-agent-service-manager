package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
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
