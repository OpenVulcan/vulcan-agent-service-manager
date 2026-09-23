//go:build windows

package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildManager creates one real manager executable with the requested embedded version.
// buildManager 构建一个嵌入指定版本号的真实管理器可执行文件。
func buildManager(t *testing.T, output, version string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-ldflags=-X github.com/OpenVulcan/vulcan-agent-service-manager/internal/selfupdate.Version="+version, "-o", output, "../../cmd/vasm")
	if text, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build manager: %v: %s", err, text)
	}
}

// TestApplyWindowsReplacesAndRollsBack verifies both post-exit replacement and launch-smoke rollback.
// TestApplyWindowsReplacesAndRollsBack 验证退出后的替换及启动冒烟失败时的回滚。
func TestApplyWindowsReplacesAndRollsBack(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		stageVersion  string
		wantFailure   bool
		wantInstalled string
	}{
		{name: "success", stageVersion: "0.1.1", wantInstalled: "0.1.1"},
		{name: "rollback", stageVersion: "0.1.2", wantFailure: true, wantInstalled: "0.1.0"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "vasm.exe")
			buildManager(t, destination, "0.1.0")
			workspace := filepath.Join(root, ".vasm-update-test")
			stage := filepath.Join(workspace, "stage")
			if err := os.MkdirAll(stage, 0o700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(stage, "vasm.exe")
			buildManager(t, source, testCase.stageVersion)
			backup := filepath.Join(workspace, "previous.exe")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, source, "--internal-apply-self-update", "1", destination, backup, source, "v0.1.1")
			output, err := command.CombinedOutput()
			if (err != nil) != testCase.wantFailure {
				t.Fatalf("helper error=%v output=%s", err, output)
			}
			versionOutput, err := exec.Command(destination, "--version").Output()
			if err != nil || strings.TrimSpace(string(versionOutput)) != "vasm "+testCase.wantInstalled {
				t.Fatalf("installed version=%q error=%v", versionOutput, err)
			}
			receipt, err := LastResult(destination)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := "success"
			if testCase.wantFailure {
				wantStatus = "failed"
			}
			if receipt.Status != wantStatus {
				t.Fatalf("receipt=%+v", receipt)
			}
		})
	}
}
