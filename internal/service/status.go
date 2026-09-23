package service

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// windowsState locates the numeric SCM state emitted by the installed service's status command.
// windowsState 定位已安装服务状态命令输出的数字式 SCM 状态。
var windowsState = regexp.MustCompile(`(?m)^\s*STATE\s*:\s*([0-9]+)\b`)

// launchdState locates the job state in launchctl print output forwarded by the service.
// launchdState 定位服务程序转发的 launchctl print 输出中的作业状态。
var launchdState = regexp.MustCompile(`(?m)^\s*state\s*=\s*([a-z-]+)\s*$`)

// Running queries the installed native service and returns whether it is actively running.
// Running 查询已安装的本机服务并返回它是否正在运行；未知状态会显式报错。
func Running(ctx context.Context, record state.Record) (bool, error) {
	status, err := Lifecycle(ctx, record, "status")
	if err != nil {
		return false, err
	}
	return ParseRunning(runtime.GOOS, status)
}

// ParseRunning interprets only status formats confirmed in the current service implementation.
// ParseRunning 仅解释当前服务程序实现中已确认的状态格式，参数为平台和状态文本。
func ParseRunning(goos, status string) (bool, error) {
	switch goos {
	case "windows":
		match := windowsState.FindStringSubmatch(status)
		if len(match) != 2 {
			return false, fmt.Errorf("Windows service status has no numeric STATE field")
		}
		switch match[1] {
		case "1":
			return false, nil
		case "4":
			return true, nil
		default:
			return false, fmt.Errorf("Windows service is transitioning or in unknown state %s", match[1])
		}
	case "linux":
		for _, line := range strings.Split(status, "\n") {
			if strings.HasPrefix(line, "active: ") {
				switch strings.TrimSpace(strings.TrimPrefix(line, "active: ")) {
				case "active":
					return true, nil
				case "inactive", "failed":
					return false, nil
				}
			}
		}
		return false, fmt.Errorf("systemd service status has no recognized active value")
	case "darwin":
		if strings.Contains(status, "loaded: false\nstatus: not-loaded") {
			return false, nil
		}
		match := launchdState.FindStringSubmatch(status)
		if len(match) != 2 {
			return false, fmt.Errorf("launchd service status has no recognized state")
		}
		switch match[1] {
		case "running":
			return true, nil
		case "waiting", "stopped":
			return false, nil
		default:
			return false, fmt.Errorf("launchd service has unknown state %s", match[1])
		}
	default:
		return false, fmt.Errorf("unsupported service platform %s", goos)
	}
}
