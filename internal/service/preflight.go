package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// serviceNamePattern constrains a native registration name before it enters commands or file paths.
// serviceNamePattern 在服务名称进入命令或文件路径前限制其格式。
var serviceNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// ValidateName rejects service names that could escape native manager namespaces.
// ValidateName 拒绝可能越出本机服务管理命名空间的名称。
func ValidateName(name string) error {
	if !serviceNamePattern.MatchString(name) {
		return fmt.Errorf("invalid native service name %q", name)
	}
	return nil
}

// EnsureAbsent proves that a first-time native service installation will not replace an existing service.
// EnsureAbsent 在首次安装本机服务前确认没有同名注册项，无法判定时返回错误。
func EnsureAbsent(ctx context.Context, record state.Record) error {
	if err := ValidateName(record.ServiceName); err != nil {
		return err
	}
	if record.ServiceScope != "user" && record.ServiceScope != "system" {
		return errors.New("native service scope must be user or system")
	}
	switch runtime.GOOS {
	case "windows":
		output, err := exec.CommandContext(ctx, "sc.exe", "query", record.ServiceName).CombinedOutput()
		if err == nil {
			return fmt.Errorf("native service %s already exists", record.ServiceName)
		}
		if !strings.Contains(string(output), "FAILED 1060:") {
			return fmt.Errorf("cannot confirm native service is absent: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	case "linux":
		unitPath, err := nativeDefinitionPath(record)
		if err != nil {
			return err
		}
		if err := requireAbsentFile(unitPath); err != nil {
			return err
		}
		args := []string{}
		if record.ServiceScope == "user" {
			args = append(args, "--user")
		}
		args = append(args, "show", "--property=LoadState", "--value", record.ServiceName+".service")
		output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot inspect systemd unit: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if strings.TrimSpace(string(output)) != "not-found" {
			return fmt.Errorf("native service %s already exists or status is unknown", record.ServiceName)
		}
		return nil
	case "darwin":
		plistPath, err := nativeDefinitionPath(record)
		if err != nil {
			return err
		}
		if err := requireAbsentFile(plistPath); err != nil {
			return err
		}
		status, err := Lifecycle(ctx, record, "status")
		if err != nil {
			return err
		}
		if !strings.Contains(status, "loaded: false\nstatus: not-loaded") {
			return fmt.Errorf("native service %s already exists or status is unknown", record.ServiceName)
		}
		return nil
	default:
		return fmt.Errorf("unsupported service platform %s", runtime.GOOS)
	}
}

// VerifyRegistration confirms an adopted native service has its actual platform registration.
// VerifyRegistration 确认接管的本机服务确实拥有对应平台的注册项。
func VerifyRegistration(ctx context.Context, record state.Record) error {
	if err := ValidateName(record.ServiceName); err != nil {
		return err
	}
	if record.ServiceScope != "user" && record.ServiceScope != "system" {
		return errors.New("native service scope must be user or system")
	}
	if runtime.GOOS != "windows" {
		definition, err := nativeDefinitionPath(record)
		if err != nil {
			return err
		}
		info, err := os.Lstat(definition)
		if err != nil {
			return fmt.Errorf("native service definition is unavailable: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("native service definition is not a regular file")
		}
	}
	if _, err := Lifecycle(ctx, record, "status"); err != nil {
		return fmt.Errorf("native service status could not be verified: %w", err)
	}
	return nil
}

// nativeDefinitionPath returns the exact unit or plist path used by the service release.
// nativeDefinitionPath 返回服务发布版本使用的准确 unit 或 plist 路径。
func nativeDefinitionPath(record state.Record) (string, error) {
	switch runtime.GOOS {
	case "linux":
		if record.ServiceScope == "system" {
			return filepath.Join("/etc/systemd/system", record.ServiceName+".service"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "systemd", "user", record.ServiceName+".service"), nil
	case "darwin":
		if record.ServiceScope == "system" {
			return filepath.Join("/Library/LaunchDaemons", record.ServiceName+".plist"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "LaunchAgents", record.ServiceName+".plist"), nil
	default:
		return "", fmt.Errorf("platform %s has no unit or plist definition path", runtime.GOOS)
	}
}

// requireAbsentFile rejects a preexisting platform service definition without following links.
// requireAbsentFile 不跟随链接地拒绝已存在的平台服务定义文件。
func requireAbsentFile(path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return fmt.Errorf("native service definition already exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
