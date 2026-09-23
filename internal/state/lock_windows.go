//go:build windows

package state

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLock acquires a nonblocking Windows file lock that survives a stale marker file.
// tryLock 获取非阻塞 Windows 文件锁，即使标记文件残留也可以在进程退出后重新获取。
func tryLock(file *os.File) (func(), error) {
	// overlapped must remain alive until the matching unlock operation completes.
	// overlapped 必须保持存活，直到对应的解锁操作完成。
	overlapped := &windows.Overlapped{}
	handle := windows.Handle(file.Fd())
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped); err != nil {
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped) }, nil
}
