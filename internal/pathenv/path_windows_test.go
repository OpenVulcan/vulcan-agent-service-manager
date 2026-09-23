//go:build windows

package pathenv

import "testing"

// TestAddRejectsSemicolonBeforeRegistryWrite prevents one directory from becoming multiple PATH entries.
// TestAddRejectsSemicolonBeforeRegistryWrite 防止单个目录被拆成多个 PATH 条目。
func TestAddRejectsSemicolonBeforeRegistryWrite(t *testing.T) {
	if _, err := Add(`C:\vasm;other`); err == nil {
		t.Fatal("semicolon-containing PATH directory was accepted")
	}
}
