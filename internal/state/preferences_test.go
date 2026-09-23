package state

import (
	"path/filepath"
	"testing"
)

// TestPreferencesSurviveServiceStateRemoval checks independent manager choice persistence.
// TestPreferencesSurviveServiceStateRemoval 检查删除服务状态后管理器选择仍然存在。
func TestPreferencesSurviveServiceStateRemoval(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	initial, err := LoadPreferences(file)
	if err != nil || initial.Source != "github" {
		t.Fatalf("unexpected defaults: %+v, %v", initial, err)
	}
	initial.Source = "mirror"
	initial.MirrorBase = "https://gh-proxy.com"
	initial.PathDirectory = filepath.Join(t.TempDir(), "bin")
	initial.PathAdded = true
	if err := SavePreferences(file, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPreferences(file)
	if err != nil || loaded != initial {
		t.Fatalf("preferences changed: %+v, %v", loaded, err)
	}
}
