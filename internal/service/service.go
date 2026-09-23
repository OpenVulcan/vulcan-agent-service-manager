package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// Executable returns the service binary at the verified release layout.
// Executable 返回已校验发布布局中的服务程序路径。
func Executable(root string) string {
	name := "vulcan-agent-service"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, "bin", name)
}

// Run executes one service command with an explicit runtime root and captures its output.
// Run 使用显式运行根执行一条服务命令并捕获输出。
func Run(ctx context.Context, root string, args ...string) (string, error) {
	executable := Executable(root)
	if _, err := os.Stat(executable); err != nil {
		return "", fmt.Errorf("service executable unavailable: %w", err)
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("service command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// Lifecycle invokes a native service action for an existing manager record.
// Lifecycle 对已有管理器记录执行一项本机服务生命周期操作。
func Lifecycle(ctx context.Context, record state.Record, action string) (string, error) {
	switch action {
	case "start", "stop", "restart", "status", "uninstall":
	default:
		return "", fmt.Errorf("unsupported service action %q", action)
	}
	if !record.ServiceInstalled && action != "status" {
		return "", fmt.Errorf("native service is not installed")
	}
	args := []string{"service", action, "--service-name", record.ServiceName, "--scope", record.ServiceScope}
	if action == "uninstall" {
		args = append(args, "--force")
	}
	return Run(ctx, record.RuntimeRoot, args...)
}

// Install registers a native service using the service's own platform implementation.
// Install 使用服务程序自身的平台实现注册本机服务。
func Install(ctx context.Context, record state.Record, startup string, start bool) (string, error) {
	if record.ServiceScope != "user" && record.ServiceScope != "system" {
		return "", fmt.Errorf("invalid service scope %q", record.ServiceScope)
	}
	if startup != "auto" && startup != "manual" {
		return "", fmt.Errorf("invalid startup policy %q", startup)
	}
	if err := EnsureAbsent(ctx, record); err != nil {
		return "", err
	}
	args := []string{"service", "install", "--runtime-root", record.RuntimeRoot, "--service-name", record.ServiceName, "--scope", record.ServiceScope, "--startup", startup}
	if start {
		args = append(args, "--start")
	}
	output, err := Run(ctx, record.RuntimeRoot, args...)
	if err != nil {
		// The service command may fail after creating its native registration.
		// 服务命令可能在创建本机注册项后失败，因此只清理预检确认原本不存在的名称。
		registered := record
		registered.ServiceInstalled = true
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, cleanupErr := Lifecycle(cleanupCtx, registered, "uninstall")
		if cleanupErr != nil {
			return output, fmt.Errorf("native service install failed: %w; cleanup also failed: %v", err, cleanupErr)
		}
		return output, err
	}
	return output, nil
}

// SetStartup changes the installed service's native automatic or manual startup policy.
// SetStartup 修改已安装服务的本机自动或手动启动策略。
func SetStartup(ctx context.Context, record state.Record, startup string) (string, error) {
	if !record.ServiceInstalled {
		return "", fmt.Errorf("native service is not installed")
	}
	if startup != "auto" && startup != "manual" {
		return "", fmt.Errorf("startup must be auto or manual")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		mode := "demand"
		if startup == "auto" {
			mode = "auto"
		}
		command = exec.CommandContext(ctx, "sc.exe", "config", record.ServiceName, "start=", mode)
	case "linux":
		args := []string{}
		if record.ServiceScope == "user" {
			args = append(args, "--user")
		}
		action := "disable"
		if startup == "auto" {
			action = "enable"
		}
		command = exec.CommandContext(ctx, "systemctl", append(args, action, record.ServiceName)...)
	case "darwin":
		// The service owns its launchd plist and force reinstall regenerates RunAtLoad safely.
		// 服务程序拥有 launchd plist，强制重装可安全重建 RunAtLoad 设置。
		args := []string{"service", "install", "--runtime-root", record.RuntimeRoot, "--service-name", record.ServiceName, "--scope", record.ServiceScope, "--startup", startup, "--force"}
		if startup == "auto" {
			args = append(args, "--start")
		}
		return Run(ctx, record.RuntimeRoot, args...)
	default:
		return "", fmt.Errorf("unsupported service platform %s", runtime.GOOS)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("set native startup: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
