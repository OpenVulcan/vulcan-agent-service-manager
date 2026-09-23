//go:build !windows

package service

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// registrationMatches reports presence and ownership of a failed install's unit or plist.
// registrationMatches 报告失败安装后的 unit 或 plist 是否存在以及是否归本次安装所有。
func registrationMatches(ctx context.Context, record state.Record, startup string) (bool, bool, error) {
	present, actual, rendered, err := registrationDefinitions(ctx, record, startup)
	if err != nil || !present {
		return present, false, err
	}
	return true, strings.TrimSpace(actual) == strings.TrimSpace(rendered), nil
}

// registrationAdoptMatches checks the service command while allowing documented display and startup settings.
// registrationAdoptMatches 校验服务命令，同时允许已定义的显示信息和启动策略差异。
func registrationAdoptMatches(ctx context.Context, record state.Record) (bool, bool, error) {
	present, actual, rendered, err := registrationDefinitions(ctx, record, "manual")
	if err != nil || !present {
		return present, false, err
	}
	if strings.TrimSpace(actual) == strings.TrimSpace(rendered) {
		return true, true, nil
	}
	if runtime.GOOS == "linux" {
		// The service CLI permits a custom description and startup policy in its fixed unit template.
		// 服务 CLI 的固定 unit 模板允许自定义描述和启动策略，因此仅排除这两个字段后比对其余内容。
		return true, normalizedSystemdDefinition(actual) == normalizedSystemdDefinition(rendered), nil
	}
	_, _, automatic, err := registrationDefinitions(ctx, record, "auto")
	if err != nil {
		return true, false, err
	}
	return true, strings.TrimSpace(actual) == strings.TrimSpace(automatic), nil
}

// normalizedSystemdDefinition removes only source-defined display and startup fields from a unit comparison.
// normalizedSystemdDefinition 只从 unit 比对中排除源码定义的显示信息与启动策略字段。
func normalizedSystemdDefinition(definition string) string {
	lines := strings.Split(strings.TrimSpace(definition), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "Description=") || strings.HasPrefix(line, "WantedBy=") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// registrationDefinitions reads the native definition and its application-generated expectation.
// registrationDefinitions 读取本机定义及应用程序生成的预期定义。
func registrationDefinitions(ctx context.Context, record state.Record, startup string) (bool, string, string, error) {
	definition, err := nativeDefinitionPath(record)
	if err != nil {
		return false, "", "", err
	}
	info, err := os.Lstat(definition)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", "", nil
	}
	if err != nil {
		return false, "", "", err
	}
	if !info.Mode().IsRegular() {
		return true, "", "", errors.New("native service definition is not a regular file")
	}
	preview, err := Run(ctx, record.RuntimeRoot, "service", "print-definition", "--runtime-root", record.RuntimeRoot, "--service-name", record.ServiceName, "--scope", record.ServiceScope, "--startup", startup)
	if err != nil {
		return true, "", "", err
	}
	_, rendered, found := strings.Cut(preview, "\ndefinition:\n")
	if !found {
		return true, "", "", errors.New("service definition preview omitted the expected definition")
	}
	actual, err := os.ReadFile(definition)
	if err != nil {
		return true, "", "", err
	}
	return true, string(actual), rendered, nil
}
