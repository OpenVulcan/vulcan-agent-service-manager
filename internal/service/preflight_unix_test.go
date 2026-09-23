//go:build !windows

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// TestVerifyRegistrationRejectsMissingDefinition prevents adopting a service that only reports not loaded.
// TestVerifyRegistrationRejectsMissingDefinition 防止接管仅报告未加载、实际没有定义文件的服务。
func TestVerifyRegistrationRejectsMissingDefinition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	record := state.Record{ServiceName: "VasmMissingService", ServiceScope: "user", ServiceInstalled: true}
	if err := VerifyRegistration(context.Background(), record); err == nil {
		t.Fatal("missing native service definition was accepted")
	}
	present, owned, err := registrationMatches(context.Background(), record, "manual")
	if err != nil || present || owned {
		t.Fatalf("missing native registration appeared to belong to this install: present=%t owned=%t error=%v", present, owned, err)
	}
}

// TestRegistrationMatchesOnlyRenderedDefinition protects another unit or plist from failed-install cleanup.
// TestRegistrationMatchesOnlyRenderedDefinition 防止失败安装清理其他来源的 unit 或 plist。
func TestRegistrationMatchesOnlyRenderedDefinition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(t.TempDir(), "service")
	binary := Executable(root)
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/bin/sh\nprintf 'manager: test\\ndefinition:\\n[Unit]\\nDescription=Owned\\n'\n")
	if err := os.WriteFile(binary, script, 0o755); err != nil {
		t.Fatal(err)
	}
	record := state.Record{RuntimeRoot: root, ServiceName: "VasmOwnedService", ServiceScope: "user", ServiceInstalled: true}
	definition, err := nativeDefinitionPath(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(definition), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definition, []byte("[Unit]\nDescription=Owned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	present, owned, err := registrationMatches(context.Background(), record, "manual")
	if err != nil || !present || !owned {
		t.Fatalf("matching registration was not recognized: present=%t owned=%t error=%v", present, owned, err)
	}
	if err := os.WriteFile(definition, []byte("[Unit]\nDescription=Foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	present, owned, err = registrationMatches(context.Background(), record, "manual")
	if err != nil || !present || owned {
		t.Fatalf("foreign registration was accepted as owned: present=%t owned=%t error=%v", present, owned, err)
	}
}
