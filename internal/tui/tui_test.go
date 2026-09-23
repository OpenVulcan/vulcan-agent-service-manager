package tui

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"
)

// TestWizardRoutesChoicesToInstall checks the visible final row constructs the selected CLI operation.
// TestWizardRoutesChoicesToInstall 检查可见的向导最后一行构造出用户选择的命令行操作。
func TestWizardRoutesChoicesToInstall(t *testing.T) {
	commands := make(chan []string, 1)
	// runner captures the command without touching the network or filesystem.
	// runner 捕获命令，不访问网络或文件系统。
	runner := func(_ context.Context, command []string, _ io.Writer) error {
		commands <- command
		return nil
	}
	initial := model{ctx: context.Background(), runner: runner, page: "wizard", cursor: 11, source: 2, mirror: "https://gh-proxy.com", version: 1, tag: "v0.1.0", root: "/tmp/vasm-test", vmm: true, vmmURL: "http://127.0.0.1:17625", mode: 2, autostart: true, initSkills: true, skillNames: "vulcan-file", addPath: true}
	if len(initial.wizardRows()) != 12 {
		t.Fatal("wizard row count changed without routing update")
	}
	_, _ = initial.updateWizard("enter")
	want := []string{"install", "--yes", "--source", "mirror", "--mirror-base", "https://gh-proxy.com", "--app-version", "v0.1.0", "--runtime-root", "/tmp/vasm-test", "--vmm", "true", "--vmm-url", "http://127.0.0.1:17625", "--init-skills", "--skills", "vulcan-file", "--add-path", "--service", "--scope", "system", "--startup", "auto"}
	select {
	case actual := <-commands:
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("wizard command mismatch:\nactual=%v\nwant=%v", actual, want)
		}
	case <-time.After(time.Second):
		t.Fatal("wizard did not dispatch the install command")
	}
}

// TestHomeRoutesPathManagement verifies the manager PATH page remains reachable after menu edits.
// TestHomeRoutesPathManagement 验证菜单调整后仍可进入管理器 PATH 页面。
func TestHomeRoutesPathManagement(t *testing.T) {
	initial := model{page: "home", cursor: 7}
	updated, _ := initial.selectMenu()
	selected := updated.(model)
	if selected.page != "path" || selected.cursor != 0 {
		t.Fatalf("home route went to %s at %d", selected.page, selected.cursor)
	}
}
