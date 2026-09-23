//go:build linux

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// TestRegistrationAdoptMatchesCustomDescriptionAndStartup verifies source-defined unit customization remains adoptable.
// TestRegistrationAdoptMatchesCustomDescriptionAndStartup 校验源码允许的描述与启动策略定制仍可接管。
func TestRegistrationAdoptMatchesCustomDescriptionAndStartup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(t.TempDir(), "service")
	binary := Executable(root)
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	record := state.Record{RuntimeRoot: root, ServiceName: "VasmAdoptService", ServiceScope: "user", ServiceInstalled: true}
	expected := fmt.Sprintf("[Unit]\nDescription=Vulcan Agent Service\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=%s service run --runtime-root %s\nWorkingDirectory=%s\nRestart=on-failure\nRestartSec=3\n\n[Install]\nWantedBy=default.target\n", binary, root, root)
	script := "#!/bin/sh\ncat <<'EOF'\nmanager: systemd\ndefinition:\n" + expected + "EOF\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := nativeDefinitionPath(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(definition), 0o700); err != nil {
		t.Fatal(err)
	}
	actual := strings.Replace(expected, "Description=Vulcan Agent Service", "Description=My Service", 1)
	actual = strings.Replace(actual, "WantedBy=default.target", "WantedBy=multi-user.target", 1)
	if err := os.WriteFile(definition, []byte(actual), 0o600); err != nil {
		t.Fatal(err)
	}
	present, owned, err := registrationAdoptMatches(context.Background(), record)
	if err != nil || !present || !owned {
		t.Fatalf("customized owned service rejected: present=%t owned=%t error=%v", present, owned, err)
	}
	if err := os.WriteFile(definition, []byte(strings.Replace(actual, binary, "/other/service", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	present, owned, err = registrationAdoptMatches(context.Background(), record)
	if err != nil || !present || owned {
		t.Fatalf("foreign service accepted: present=%t owned=%t error=%v", present, owned, err)
	}
}
