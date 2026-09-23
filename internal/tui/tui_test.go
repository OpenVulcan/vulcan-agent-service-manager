package tui

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

// TestWizardRoutesChoicesToInstall checks the package download precedes settings and pins the selected Release.
// TestWizardRoutesChoicesToInstall 检查先下载发布包再配置安装，并固定选定的发布版本。
func TestWizardRoutesChoicesToInstall(t *testing.T) {
	commands := make(chan []string, 2)
	// runner simulates the verified package response without touching the network or filesystem.
	// runner 模拟已校验发布包响应，不访问网络或文件系统。
	runner := func(_ context.Context, command []string, output io.Writer) error {
		commands <- command
		if command[0] == "--internal-prepare-install" {
			_, _ = io.WriteString(output, `{"tag":"v0.1.0","archive_path":"/tmp/vasm-test/verified.tar.gz","asset_name":"verified.tar.gz","asset_size":100}`)
		}
		return nil
	}
	initial := model{ctx: context.Background(), runner: runner, page: "wizard", cursor: 3, source: 2, mirror: "https://gh-proxy.com", version: 1, tag: "v0.1.0", root: "/tmp/vasm-test", vmm: true, vmmURL: "http://127.0.0.1:17625", mode: 2, autostart: true, initSkills: true, skillNames: "vulcan-file", addPath: true}
	if len(initial.wizardRows()) != 4 {
		t.Fatal("download stage row count changed without routing update")
	}
	updated, wait := initial.updateWizard("enter")
	prepared := updated.(model)
	prepareWant := []string{"--internal-prepare-install", "--source", "mirror", "--mirror-base", "https://gh-proxy.com", "--app-version", "v0.1.0"}
	select {
	case actual := <-commands:
		if !reflect.DeepEqual(actual, prepareWant) {
			t.Fatalf("prepare command mismatch: actual=%v want=%v", actual, prepareWant)
		}
	case <-time.After(time.Second):
		t.Fatal("wizard did not dispatch the package download")
	}
	configured := prepared
	for attempt := 0; attempt < 3 && configured.page != "wizard"; attempt++ {
		stage, next := configured.Update(wait())
		configured = stage.(model)
		wait = next
	}
	if configured.page != "wizard" || len(configured.wizardRows()) != 9 || configured.preparedTag != "v0.1.0" {
		t.Fatalf("verified package did not open the settings stage: %+v", configured)
	}
	configured.cursor = 8
	confirmation, _ := configured.updateWizard("enter")
	selected := confirmation.(model)
	want := []string{"install", "--yes", "--app-version", "v0.1.0", "--prepared-archive", "/tmp/vasm-test/verified.tar.gz", "--source", "mirror", "--mirror-base", "https://gh-proxy.com", "--runtime-root", "/tmp/vasm-test", "--vmm", "true", "--vmm-url", "http://127.0.0.1:17625", "--init-skills", "--skills", "vulcan-file", "--add-path", "--service", "--scope", "system", "--startup", "auto"}
	if selected.page != "confirm" || !reflect.DeepEqual(selected.pendingCommand, want) {
		t.Fatalf("wizard did not preview the selected install command: page=%s actual=%v want=%v", selected.page, selected.pendingCommand, want)
	}
}

// TestHomeRoutesPathManagement verifies the manager PATH page remains reachable after menu edits.
// TestHomeRoutesPathManagement 验证菜单调整后仍可进入管理器 PATH 页面。
func TestHomeRoutesPathManagement(t *testing.T) {
	initial := model{page: "home", cursor: 9}
	updated, _ := initial.selectMenu()
	selected := updated.(model)
	if selected.page != "path" || selected.cursor != 0 {
		t.Fatalf("home route went to %s at %d", selected.page, selected.cursor)
	}
}

// TestMenusExposeRootInstallAndNativeAdoption keeps explicit skill and existing-service actions reachable.
// TestMenusExposeRootInstallAndNativeAdoption 确保系统技能安装及既有服务接管入口可达。
func TestMenusExposeRootInstallAndNativeAdoption(t *testing.T) {
	rootSkill, _ := (model{page: "skills", cursor: 1}).selectMenu()
	selectedSkill := rootSkill.(model)
	if selectedSkill.page != "input" || !reflect.DeepEqual(selectedSkill.inputCommand, []string{"__root_install"}) {
		t.Fatalf("ROOT installation menu did not open its input: %+v", selectedSkill)
	}
	serviceAdopt, _ := (model{page: "home", cursor: 4}).selectMenu()
	selectedAdopt := serviceAdopt.(model)
	if selectedAdopt.page != "input" || !reflect.DeepEqual(selectedAdopt.inputCommand, []string{"__adopt", "system"}) {
		t.Fatalf("system service adoption menu did not open its input: %+v", selectedAdopt)
	}
}

// TestRunReportsSourceLoadFailure prevents a damaged preference file from appearing as GitHub mode.
// TestRunReportsSourceLoadFailure 防止损坏的偏好文件被显示为 GitHub 模式。
func TestRunReportsSourceLoadFailure(t *testing.T) {
	want := errors.New("invalid preferences")
	// runner simulates a damaged saved source before terminal initialization.
	// runner 在终端初始化前模拟已保存下载源损坏。
	runner := func(_ context.Context, _ []string, _ io.Writer) error { return want }
	if err := Run(context.Background(), runner); !errors.Is(err, want) {
		t.Fatalf("source load error was hidden: %v", err)
	}
}
