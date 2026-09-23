//go:build !windows

package service

import (
	"context"
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
}
