package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
)

// TestCopyPreparedArchiveRejectsTampering ensures a package changed after prefetch cannot be installed.
// TestCopyPreparedArchiveRejectsTampering 确保预取后被改动的发布包无法进入安装流程。
func TestCopyPreparedArchiveRejectsTampering(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "download.tar.gz")
	assetBytes := []byte("official archive")
	digest := sha256.Sum256(assetBytes)
	asset := release.Asset{Name: "download.tar.gz", Size: int64(len(assetBytes)), SHA256: hex.EncodeToString(digest[:])}
	if err := os.WriteFile(source, assetBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyPreparedArchive(source, filepath.Join(directory, "verified.tar.gz"), asset); err != nil {
		t.Fatalf("official package was rejected: %v", err)
	}
	if err := os.WriteFile(source, []byte("changed archive!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyPreparedArchive(source, filepath.Join(directory, "tampered.tar.gz"), asset); err == nil {
		t.Fatal("tampered package was accepted")
	}
}
