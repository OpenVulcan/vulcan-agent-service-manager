package platform

import (
	"fmt"
	"runtime"
)

// Target describes the published operating system, architecture, and archive format.
// Target 描述已发布的操作系统、架构及归档格式。
type Target struct {
	// Name is the stable platform identifier used in Release asset names.
	// Name 是 Release 资产名称中的稳定平台标识。
	Name string
	// Extension is the platform archive suffix.
	// Extension 是平台归档文件后缀。
	Extension string
	// Executable is the executable filename on this platform.
	// Executable 是此平台上的可执行文件名。
	Executable string
}

// Current returns the published target matching the running process, or an error for unsupported systems.
// Current 返回与当前进程匹配的发布目标；不支持的系统返回错误。
func Current() (Target, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return Target{"windows-x64", ".zip", "vasm.exe"}, nil
	case "linux/amd64":
		return Target{"linux-x64", ".tar.gz", "vasm"}, nil
	case "linux/arm64":
		return Target{"linux-arm64", ".tar.gz", "vasm"}, nil
	case "darwin/amd64":
		return Target{"macos-x64", ".tar.gz", "vasm"}, nil
	case "darwin/arm64":
		return Target{"macos-arm64", ".tar.gz", "vasm"}, nil
	default:
		return Target{}, fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// AppAssetName returns the exact service archive name published for tag and target.
// AppAssetName 根据标签和目标平台返回主程序归档的精确名称。
func AppAssetName(tag string, target Target) string {
	return "vulcan-agent-service-" + tag + "-" + target.Name + target.Extension
}

// ManagerAssetName returns the stable manager archive name for a target.
// ManagerAssetName 返回指定目标平台的稳定管理器归档名称。
func ManagerAssetName(target Target) string {
	return "vasm-" + target.Name + target.Extension
}
