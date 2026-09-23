//go:build !windows

package state

import (
	"os"

	"golang.org/x/sys/unix"
)

// tryLock acquires a nonblocking kernel lock that the OS releases on process exit.
// tryLock 获取非阻塞内核文件锁，进程退出时操作系统会自动释放。
func tryLock(file *os.File) (func(), error) {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, err
	}
	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }, nil
}
