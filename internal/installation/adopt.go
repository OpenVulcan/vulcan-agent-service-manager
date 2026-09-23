package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/platform"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/service"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// Adopt validates an existing service release before creating an explicit manager record.
// Adopt 在创建显式管理器记录之前校验已有服务发布版本。
func Adopt(ctx context.Context, stateFile, runtimeRoot string, serviceInstalled bool, scope, serviceName string) (state.Record, error) {
	root, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return state.Record{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return state.Record{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return state.Record{}, errors.New("existing runtime root must be a directory")
	}
	target, err := platform.Current()
	if err != nil {
		return state.Record{}, err
	}
	contents, err := os.ReadFile(filepath.Join(root, "release-manifest.json"))
	if err != nil {
		return state.Record{}, err
	}
	var manifest struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return state.Record{}, err
	}
	if err := release.ValidateTag(manifest.Tag); err != nil {
		return state.Record{}, err
	}
	if err := verifyManifest(root, manifest.Tag, target.Name, platform.AppAssetName(manifest.Tag, target)); err != nil {
		return state.Record{}, fmt.Errorf("cannot adopt unverified service package: %w", err)
	}
	if scope != "user" && scope != "system" {
		return state.Record{}, errors.New("service scope must be user or system")
	}
	if runtime.GOOS == "windows" && serviceInstalled && scope != "system" {
		return state.Record{}, errors.New("Windows service scope must be system")
	}
	if serviceName == "" {
		serviceName = "VulcanAgentService"
	}
	if err := service.ValidateName(serviceName); err != nil {
		return state.Record{}, err
	}
	record := state.Record{SchemaVersion: 1, RuntimeRoot: root, AppTag: manifest.Tag, Source: "github", ServiceName: serviceName, ServiceScope: scope, ServiceInstalled: serviceInstalled, Managed: true}
	if serviceInstalled {
		if err := service.VerifyRegistration(ctx, record); err != nil {
			return state.Record{}, fmt.Errorf("declared service registration could not be verified: %w", err)
		}
	}
	unlock, err := state.Lock(stateFile)
	if err != nil {
		return state.Record{}, err
	}
	defer unlock()
	if _, err := os.Stat(stateFile); err == nil {
		return state.Record{}, errors.New("manager record already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return state.Record{}, err
	}
	if err := state.Save(stateFile, record); err != nil {
		return state.Record{}, err
	}
	return record, nil
}
