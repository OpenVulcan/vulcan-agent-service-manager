//go:build windows

package service

import (
	"context"
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// registrationMatches reports whether an SCM registration exists and runs this exact service binary and root.
// registrationMatches 报告 SCM 注册项是否存在及是否运行本次安装的准确服务程序与运行根。
func registrationMatches(_ context.Context, record state.Record, _ string) (bool, bool, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return false, false, err
	}
	defer manager.Disconnect()
	registered, err := manager.OpenService(record.ServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	defer registered.Close()
	config, err := registered.Config()
	if err != nil {
		return true, false, err
	}
	arguments, err := windows.DecomposeCommandLine(config.BinaryPathName)
	if err != nil {
		return true, false, err
	}
	expectedLength := 5
	if record.ServiceName != "VulcanAgentService" {
		expectedLength = 7
	}
	if len(arguments) != expectedLength || !samePathObject(arguments[0], Executable(record.RuntimeRoot)) || arguments[1] != "service" || arguments[2] != "run" || arguments[3] != "--runtime-root" || !samePathObject(arguments[4], record.RuntimeRoot) {
		return true, false, nil
	}
	if expectedLength == 7 && (arguments[5] != "--service-name" || !strings.EqualFold(arguments[6], record.ServiceName)) {
		return true, false, nil
	}
	return true, true, nil
}

// registrationAdoptMatches verifies the exact Windows service command before adopting its registration.
// registrationAdoptMatches 在接管 Windows 服务注册项前校验其准确命令。
func registrationAdoptMatches(ctx context.Context, record state.Record) (bool, bool, error) {
	return registrationMatches(ctx, record, "manual")
}

// samePathObject compares two existing Windows paths independently of case and 8.3 spelling.
// samePathObject 比较两个现有 Windows 路径，不受大小写和 8.3 短路径写法影响。
func samePathObject(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}
