package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// TestSkillsCommandRejectsIgnoredArguments ensures a ROOT update never silently discards a skill ID.
// TestSkillsCommandRejectsIgnoredArguments 确保 ROOT 更新不会默默忽略用户填写的技能 ID。
func TestSkillsCommandRejectsIgnoredArguments(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "missing-state.json")
	for _, args := range [][]string{
		{"update", "specific-skill", "--layer", "ROOT"},
		{"list", "extra", "--layer", "USER"},
		{"install", "", "--layer", "ROOT"},
	} {
		err := skillsCommand(context.Background(), stateFile, args, io.Discard)
		if err == nil || strings.Contains(err.Error(), "missing-state.json") {
			t.Fatalf("invalid skill arguments were not rejected before state access: args=%v error=%v", args, err)
		}
	}
}

// TestDoctorSeparatesApplicationAndNativeService verifies diagnosis does not label a foreground install as a service.
// TestDoctorSeparatesApplicationAndNativeService 验证诊断不会把前台安装误标为本机服务。
func TestDoctorSeparatesApplicationAndNativeService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	client := release.NewClient()
	client.APIBase = server.URL
	stateFile := filepath.Join(t.TempDir(), "state.json")
	record := state.Record{SchemaVersion: 1, RuntimeRoot: filepath.Join(t.TempDir(), "app"), AppTag: "v0.1.0", Managed: true}
	if err := state.Save(stateFile, record); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := doctorCommand(context.Background(), client, stateFile, []string{"--json"}, &output); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output.String()), &result); err != nil {
		t.Fatal(err)
	}
	if result["app_installed"] != true || result["native_service_installed"] != false {
		t.Fatalf("doctor confused application and native service state: %v", result)
	}
	if err := doctorCommand(context.Background(), client, stateFile, []string{"--json", "extra"}, io.Discard); err == nil {
		t.Fatal("doctor accepted unsupported extra arguments")
	}
}
