package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Record is the manager-owned inventory for one installed service instance.
// Record 是管理器拥有的单个已安装服务实例清单。
type Record struct {
	// SchemaVersion identifies the manager state format.
	// SchemaVersion 标识管理器状态格式。
	SchemaVersion int `json:"schema_version"`
	// RuntimeRoot is the absolute installed application root.
	// RuntimeRoot 是应用安装根目录的绝对路径。
	RuntimeRoot string `json:"runtime_root"`
	// AppTag is the last committed application Release tag.
	// AppTag 是最后提交的应用发布标签。
	AppTag string `json:"app_tag"`
	// Source is github or mirror, as explicitly selected by the user.
	// Source 是用户主动选择的 github 或 mirror。
	Source string `json:"source"`
	// MirrorBase is the chosen HTTPS transfer proxy when Source is mirror.
	// MirrorBase 是选择镜像传输时的 HTTPS 代理基址。
	MirrorBase string `json:"mirror_base,omitempty"`
	// ServiceName is the native service registration name.
	// ServiceName 是本机服务的注册名称。
	ServiceName string `json:"service_name,omitempty"`
	// ServiceScope is the native user or system service scope.
	// ServiceScope 是本机服务的用户或系统作用域。
	ServiceScope string `json:"service_scope,omitempty"`
	// ServiceInstalled records whether this manager installed a native service.
	// ServiceInstalled 记录管理器是否安装了本机服务。
	ServiceInstalled bool `json:"service_installed"`
	// Startup is the manager-selected native service startup policy.
	// Startup 是管理器选择的本机服务启动策略。
	Startup string `json:"startup,omitempty"`
	// PathAdded records whether vasm itself added its directory to user PATH.
	// PathAdded 记录 vasm 自身是否把程序目录加入用户 PATH。
	PathAdded bool `json:"path_added,omitempty"`
	// Managed records whether the instance was installed or explicitly adopted by vasm.
	// Managed 记录此实例是否由 vasm 安装或显式接管。
	Managed bool `json:"managed"`
	// Uninstalling marks a previously verified removal that may need an idempotent retry.
	// Uninstalling 标记已完成身份校验、可能需要幂等重试的卸载过程。
	Uninstalling bool `json:"uninstalling,omitempty"`
	// UninstallPurge preserves the approved data-deletion choice across retries.
	// UninstallPurge 在重试之间保留用户已确认的数据删除选择。
	UninstallPurge bool `json:"uninstall_purge,omitempty"`
}

// DefaultFile returns the per-user state file path using platform conventions.
// DefaultFile 按平台约定返回当前用户的状态文件路径。
func DefaultFile() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is unset")
		}
		return filepath.Join(base, "OpenVulcan", "vasm", "state.json"), nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "openvulcan", "vasm", "state.json"), nil
}

// DefaultRuntimeRoot returns an absolute per-user service installation directory.
// DefaultRuntimeRoot 返回当前用户专用的服务安装绝对目录。
func DefaultRuntimeRoot() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is unset")
		}
		return filepath.Join(base, "OpenVulcan", "agent-service"), nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "openvulcan", "agent-service"), nil
}

// Load reads and validates a manager record; a missing file is returned as os.ErrNotExist.
// Load 读取并校验管理器记录；文件不存在时返回 os.ErrNotExist。
func Load(file string) (Record, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(contents, &record); err != nil {
		return Record{}, err
	}
	if record.SchemaVersion != 1 || !filepath.IsAbs(record.RuntimeRoot) || !record.Managed {
		return Record{}, errors.New("unsupported or invalid manager state")
	}
	return record, nil
}

// Save atomically commits a validated record to its per-user state file.
// Save 将通过校验的记录原子提交到当前用户的状态文件。
func Save(file string, record Record) error {
	if record.SchemaVersion != 1 || !filepath.IsAbs(record.RuntimeRoot) || !record.Managed {
		return errors.New("invalid manager state")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(append(contents, '\n')); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), file)
}

// Lock acquires a process-wide installation lock and returns a release callback.
// Lock 获取进程间安装锁，并返回释放锁的回调。
func Lock(file string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return nil, err
	}
	lockPath := file + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	releaseLock, err := tryLock(lockFile)
	if err != nil {
		lockFile.Close()
		return nil, fmt.Errorf("another manager operation may be active: %w", err)
	}
	if err := lockFile.Truncate(0); err != nil {
		releaseLock()
		lockFile.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(lockFile, "%d\n", os.Getpid()); err != nil {
		releaseLock()
		lockFile.Close()
		return nil, err
	}
	return func() { releaseLock(); lockFile.Close() }, nil
}
