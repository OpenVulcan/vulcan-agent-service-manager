package installation

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/appconfig"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/platform"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// fakeRelease stores one small test archive with its exact GitHub metadata.
// fakeRelease 保存一份小型测试归档及其精确的 GitHub 元数据。
type fakeRelease struct {
	// tag is the selected application version.
	// tag 是所选应用版本。
	tag string
	// assetName is the platform archive filename.
	// assetName 是平台归档文件名。
	assetName string
	// body contains the complete archive bytes.
	// body 包含完整归档字节。
	body []byte
	// digest is the archive SHA-256 in lowercase hex.
	// digest 是归档 SHA-256 的小写十六进制形式。
	digest string
}

// makeFakeRelease builds a valid native archive with a matching service manifest.
// makeFakeRelease 构建含匹配服务清单的有效原生归档。
func makeFakeRelease(t *testing.T, tag string, target platform.Target) fakeRelease {
	t.Helper()
	assetName := platform.AppAssetName(tag, target)
	binaryName := "vulcan-agent-service"
	if target.Name == "windows-x64" {
		binaryName += ".exe"
	}
	binary := []byte("fake service " + tag)
	binaryHash := sha256.Sum256(binary)
	manifest := map[string]any{
		"schema_version": 1,
		"product_name":   "vulcan-agent-service",
		"tag":            tag,
		"platform":       target.Name,
		"archive":        assetName,
		"contents":       map[string]any{"binary": "bin/" + binaryName},
		"traceability":   map[string]any{"binary_sha256": hex.EncodeToString(binaryHash[:])},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"bin/" + binaryName:           binary,
		"release-manifest.json":       manifestBytes,
		"configs/config.yaml":         []byte("vmm_enable: false\nvmm: \"http://127.0.0.1:17625\"\nhttp: \"127.0.0.1:19201\"\ngrpc: \"127.0.0.1:19202\"\n"),
		"configs/client_budgets.yaml": []byte("defaults:\n  budgets:\n    tool_result:\n      bytes:\n        default: 20000\nclients: []\n"),
		"configs/system_skills.json":  []byte("{\"format_version\":1,\"auto_install\":false,\"skills\":[]}"),
	}
	var output bytes.Buffer
	root := strings.TrimSuffix(assetName, target.Extension)
	if target.Extension == ".zip" {
		writer := zip.NewWriter(&output)
		for name, contents := range files {
			entry, err := writer.Create(root + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(contents); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		compressor := gzip.NewWriter(&output)
		writer := tar.NewWriter(compressor)
		for name, contents := range files {
			if err := writer.WriteHeader(&tar.Header{Name: root + "/" + name, Mode: 0o755, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(contents); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressor.Close(); err != nil {
			t.Fatal(err)
		}
	}
	archiveHash := sha256.Sum256(output.Bytes())
	return fakeRelease{tag: tag, assetName: assetName, body: output.Bytes(), digest: hex.EncodeToString(archiveHash[:])}
}

// TestInstallUpgradeRollback verifies state, preserved config, and rejection of tampered upgrades.
// TestInstallUpgradeRollback 验证状态、配置保留以及篡改升级的拒绝行为。
func TestInstallUpgradeRollback(t *testing.T) {
	target, err := platform.Current()
	if err != nil {
		t.Fatal(err)
	}
	first := makeFakeRelease(t, "v0.1.0", target)
	second := makeFakeRelease(t, "v0.1.1", target)
	selected := first
	corrupt := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/releases/") && strings.Contains(request.URL.Path, "/repos/") {
			payload := map[string]any{
				"tag_name": selected.tag,
				"assets": []map[string]any{{
					"name":                 selected.assetName,
					"browser_download_url": "https://github.com/OpenVulcan/vulcan-agent-service/releases/download/" + selected.tag + "/" + selected.assetName,
					"size":                 len(selected.body),
					"digest":               "sha256:" + selected.digest,
				}},
			}
			_ = json.NewEncoder(writer).Encode(payload)
			return
		}
		if corrupt {
			_, _ = io.WriteString(writer, "corrupted archive")
			return
		}
		_, _ = writer.Write(selected.body)
	}))
	defer server.Close()
	client := release.NewClient()
	client.HTTP = server.Client()
	client.APIBase = server.URL
	manager := Manager{Releases: client}
	root := filepath.Join(t.TempDir(), "service")
	stateFile := filepath.Join(t.TempDir(), "state.json")
	options := Options{StateFile: stateFile, RuntimeRoot: root, Tag: first.tag, Source: release.Source{Kind: "mirror", MirrorBase: server.URL}}
	record, err := manager.Install(context.Background(), options, nil)
	if err != nil || record.AppTag != first.tag {
		t.Fatalf("initial installation failed: record=%+v error=%v", record, err)
	}
	if err := appconfig.SetScalar(root, appconfig.Scalar{Key: "vmm_enable", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{filepath.Join("lua_runtime", "skills", "installed.txt"), filepath.Join("lua_runtime", "state", "index.db"), filepath.Join("lua_runtime", "databases", "user.db"), filepath.Join("lua_runtime", "userdata", "profile.json"), filepath.Join("lua_runtime", "config", "runtime.json"), filepath.Join("lua_runtime", "system_lua_lib", "custom.lua"), filepath.Join("logs", "service.log")} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	selected = second
	options.Tag = second.tag
	options.InitializeSkills = true
	if _, err := manager.Install(context.Background(), options, nil); err == nil {
		t.Fatal("post-commit initialization failure did not trigger rollback")
	}
	rolledBack, err := state.Load(stateFile)
	if err != nil || rolledBack.AppTag != first.tag {
		t.Fatalf("initialization failure changed the committed version: %+v, %v", rolledBack, err)
	}
	options.InitializeSkills = false
	record, err = manager.Install(context.Background(), options, nil)
	if err != nil || record.AppTag != second.tag {
		t.Fatalf("upgrade failed: record=%+v error=%v", record, err)
	}
	config, err := os.ReadFile(filepath.Join(root, "configs", "config.yaml"))
	if err != nil || !bytes.Contains(config, []byte("vmm_enable: true")) {
		t.Fatalf("user config not preserved: %s, %v", config, err)
	}
	for _, relative := range []string{filepath.Join("lua_runtime", "skills", "installed.txt"), filepath.Join("lua_runtime", "state", "index.db"), filepath.Join("lua_runtime", "databases", "user.db"), filepath.Join("lua_runtime", "userdata", "profile.json"), filepath.Join("lua_runtime", "config", "runtime.json"), filepath.Join("lua_runtime", "system_lua_lib", "custom.lua"), filepath.Join("logs", "service.log")} {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil || string(content) != "user data" {
			t.Fatalf("persistent data %s not preserved: %q, %v", relative, content, err)
		}
	}
	selected = first
	corrupt = true
	options.Tag = first.tag
	if _, err := manager.Install(context.Background(), options, nil); err == nil {
		t.Fatal("tampered upgrade was accepted")
	}
	retained, err := state.Load(stateFile)
	if err != nil || retained.AppTag != second.tag {
		t.Fatalf("failed upgrade changed committed state: %+v, %v", retained, err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", filepath.Base(serviceExecutable(root)))); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), stateFile, false); err != nil {
		t.Fatalf("keep-data uninstall failed: %v", err)
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("manager state remained after uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "configs", "config.yaml")); err != nil {
		t.Fatalf("user config was removed: %v", err)
	}
	if _, err := os.Stat(serviceExecutable(root)); !os.IsNotExist(err) {
		t.Fatalf("service binary remained after uninstall: %v", err)
	}
	unknown := filepath.Join(root, "unmanaged-user-file.txt")
	if err := os.WriteFile(unknown, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt = false
	selected = second
	options.Tag = second.tag
	if _, err := manager.Install(context.Background(), options, nil); err == nil {
		t.Fatal("reinstall accepted an unknown file that would be discarded")
	}
	if contents, err := os.ReadFile(unknown); err != nil || string(contents) != "keep me" {
		t.Fatalf("rejected reinstall changed unknown data: %q, %v", contents, err)
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	reinstalled, err := manager.Install(context.Background(), options, nil)
	if err != nil || reinstalled.AppTag != second.tag {
		t.Fatalf("reinstall from retained data failed: record=%+v error=%v", reinstalled, err)
	}
	for _, relative := range []string{filepath.Join("lua_runtime", "skills", "installed.txt"), filepath.Join("lua_runtime", "state", "index.db"), filepath.Join("lua_runtime", "databases", "user.db"), filepath.Join("lua_runtime", "userdata", "profile.json"), filepath.Join("lua_runtime", "config", "runtime.json"), filepath.Join("lua_runtime", "system_lua_lib", "custom.lua"), filepath.Join("logs", "service.log")} {
		contents, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil || string(contents) != "user data" {
			t.Fatalf("retained data %s was lost during reinstall: %q, %v", relative, contents, err)
		}
	}
}

// serviceExecutable returns the installed fake binary path for the current test platform.
// serviceExecutable 返回当前测试平台上安装的伪服务程序路径。
func serviceExecutable(root string) string {
	name := "vulcan-agent-service"
	if target, _ := platform.Current(); target.Name == "windows-x64" {
		name += ".exe"
	}
	return filepath.Join(root, "bin", name)
}

// TestManifestRejectsWrongIdentity protects against an archive for another release.
// TestManifestRejectsWrongIdentity 防止接纳另一个发布版本的归档。
func TestManifestRejectsWrongIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "release-manifest.json"), []byte(fmt.Sprintf(`{"schema_version":1,"product_name":"wrong","tag":"v0.1.0","platform":"windows-x64","archive":"x","contents":{"binary":"x"},"traceability":{"binary_sha256":"%064d"}}`, 0)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(root, "v0.1.0", "windows-x64", "x"); err == nil {
		t.Fatal("wrong product identity accepted")
	}
}

// TestUninstallResumesAfterPartialRemoval checks that a verified interrupted uninstall can finish.
// TestUninstallResumesAfterPartialRemoval 检查已校验但中断的卸载能够继续完成。
func TestUninstallResumesAfterPartialRemoval(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service")
	controller := filepath.Join(root, "lua_runtime", "bin", "vldb-controller")
	if err := os.MkdirAll(filepath.Dir(controller), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller, []byte("packaged controller"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"contents":{"binary":"bin/vulcan-agent-service"}}`)
	if err := os.WriteFile(filepath.Join(root, "release-manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(t.TempDir(), "state.json")
	record := state.Record{SchemaVersion: 1, RuntimeRoot: root, AppTag: "v0.1.0", Managed: true, Uninstalling: true}
	if err := state.Save(stateFile, record); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), stateFile, true); err == nil {
		t.Fatal("retry accepted a different purge choice")
	}
	if err := Uninstall(context.Background(), stateFile, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(controller); !os.IsNotExist(err) {
		t.Fatalf("packaged controller remained after retry: %v", err)
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("manager state remained after retry: %v", err)
	}
}
