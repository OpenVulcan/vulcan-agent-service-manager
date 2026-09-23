package installation

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUninstallPathGuard rejects traversal and linked parents before any package path is removed.
// TestUninstallPathGuard 在删除发布路径之前拒绝目录穿越和链接父目录。
func TestUninstallPathGuard(t *testing.T) {
	root := t.TempDir()
	if err := validateOwnedPath(root, filepath.Join("..", "outside")); err == nil {
		t.Fatal("parent traversal was accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this runner: %v", err)
	}
	if err := validateOwnedPath(root, filepath.Join("bin", "vasm")); err == nil {
		t.Fatal("linked package parent was accepted")
	}
}
