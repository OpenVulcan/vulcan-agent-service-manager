package state

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLockRejectsConcurrentOperationAndReusesMarker verifies exclusive locking and stale-file recovery.
// TestLockRejectsConcurrentOperationAndReusesMarker 验证互斥锁和残留标记文件恢复。
func TestLockRejectsConcurrentOperationAndReusesMarker(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(file+".lock", []byte("stale PID\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := Lock(file)
	if err != nil {
		t.Fatalf("stale marker prevented lock acquisition: %v", err)
	}
	if _, err := Lock(file); err == nil {
		t.Fatal("second operation acquired a held lock")
	}
	release()
	releaseAgain, err := Lock(file)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	releaseAgain()
}
