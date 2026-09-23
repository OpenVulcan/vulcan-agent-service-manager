//go:build !windows

package pathenv

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAddRefusesLinkedProfile keeps an existing shell profile symlink and its target intact.
// TestAddRefusesLinkedProfile 保留已有 shell 配置文件链接及其目标内容。
func TestAddRefusesLinkedProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	target := filepath.Join(home, "shared-profile")
	if err := os.WriteFile(target, []byte("# user settings\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(home, ".profile")
	if err := os.Symlink(target, profilePath); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(filepath.Join(home, "bin")); err == nil {
		t.Fatal("linked shell profile was replaced")
	}
	info, err := os.Lstat(profilePath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("profile link was changed: %v, %v", info, err)
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "# user settings\n" {
		t.Fatalf("profile target changed: %q, %v", contents, err)
	}
}
