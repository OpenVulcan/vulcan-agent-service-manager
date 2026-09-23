package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/appconfig"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/archive"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/platform"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/service"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// Options describes one fully specified service installation or upgrade.
// Options 描述一次参数完整的服务安装或升级。
type Options struct {
	// StateFile is the manager-owned record path.
	// StateFile 是管理器持有的记录路径。
	StateFile string
	// RuntimeRoot is the absolute installation destination.
	// RuntimeRoot 是安装目标绝对路径。
	RuntimeRoot string
	// Tag is an optional fixed Release tag; empty selects latest.
	// Tag 是可选的固定发布标签；空值选择最新版。
	Tag string
	// Source chooses GitHub transfer or an explicit HTTPS mirror.
	// Source 选择 GitHub 传输或显式 HTTPS 镜像。
	Source release.Source
	// InitializeSkills runs the existing service init command after installation.
	// InitializeSkills 在安装后执行服务程序已有的 init 命令。
	InitializeSkills bool
	// SkillNames optionally replaces the packaged ROOT skill enabled set before init.
	// SkillNames 可选地在 init 之前替换发布包中 ROOT 技能的启用集合；nil 保留发布默认值。
	SkillNames []string
	// VMMEnabled optionally sets the existing vmm_enable field.
	// VMMEnabled 可选地设置已有的 vmm_enable 字段。
	VMMEnabled *bool
	// VMMEndpoint optionally sets the existing vmm field.
	// VMMEndpoint 可选地设置已有的 vmm 字段。
	VMMEndpoint string
	// InstallService selects a native service for a first install.
	// InstallService 在首次安装时选择本机服务模式。
	InstallService bool
	// ServiceScope is user or system for a native service.
	// ServiceScope 是本机服务的 user 或 system 作用域。
	ServiceScope string
	// Startup is auto or manual for a native service.
	// Startup 是本机服务的 auto 或 manual 启动策略。
	Startup string
	// StartService requests immediate service start after installation.
	// StartService 要求在安装后立即启动服务。
	StartService bool
	// UpgradeOnly skips an unchanged version and refuses an older latest Release.
	// UpgradeOnly 跳过相同版本，并拒绝较旧的最新版发布。
	UpgradeOnly bool
}

// Manager performs verified downloads and transactional service installations.
// Manager 执行经校验的下载及事务式服务安装。
type Manager struct {
	// Releases is the GitHub metadata and asset transfer client.
	// Releases 是 GitHub 元数据与资产传输客户端。
	Releases *release.Client
}

// Install downloads, verifies, stages, and commits one service Release with rollback.
// Install 下载、校验、暂存并提交一个服务发布版本，失败时执行回滚。
func (m *Manager) Install(ctx context.Context, options Options, progress func(string, int64, int64)) (state.Record, error) {
	if !filepath.IsAbs(options.RuntimeRoot) || options.StateFile == "" {
		return state.Record{}, errors.New("absolute runtime root and state file are required")
	}
	if err := release.ValidateSource(options.Source); err != nil {
		return state.Record{}, err
	}
	if options.InstallService && runtime.GOOS == "windows" && options.ServiceScope != "system" {
		return state.Record{}, errors.New("Windows native service requires system scope and administrator privileges")
	}
	if options.InstallService && (options.Startup != "auto" && options.Startup != "manual") {
		return state.Record{}, errors.New("native service startup must be auto or manual")
	}
	if options.InstallService && options.ServiceScope != "user" && options.ServiceScope != "system" {
		return state.Record{}, errors.New("native service scope must be user or system")
	}
	if options.StartService && !options.InstallService {
		return state.Record{}, errors.New("starting a service requires native service installation")
	}
	target, err := platform.Current()
	if err != nil {
		return state.Record{}, err
	}
	releaseInfo, err := m.Releases.Fetch(ctx, release.ServiceRepository, options.Tag)
	if err != nil {
		return state.Record{}, err
	}
	assetName := platform.AppAssetName(releaseInfo.Tag, target)
	asset, err := releaseInfo.FindAsset(assetName)
	if err != nil {
		return state.Record{}, err
	}
	unlock, err := state.Lock(options.StateFile)
	if err != nil {
		return state.Record{}, err
	}
	defer unlock()
	previous, readErr := state.Load(options.StateFile)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return state.Record{}, readErr
	}
	hasPrevious := readErr == nil
	if hasPrevious && filepath.Clean(previous.RuntimeRoot) != filepath.Clean(options.RuntimeRoot) {
		return state.Record{}, errors.New("existing manager record points to a different runtime root")
	}
	if hasPrevious && options.UpgradeOnly {
		comparison, err := release.CompareTags(releaseInfo.Tag, previous.AppTag)
		if err != nil {
			return state.Record{}, err
		}
		if comparison < 0 {
			return state.Record{}, errors.New("selected Release is older than the installed version")
		}
		if comparison == 0 {
			if _, err := os.Stat(service.Executable(previous.RuntimeRoot)); err != nil {
				return state.Record{}, fmt.Errorf("installed version matches latest but its binary is missing: %w", err)
			}
			if progress != nil {
				progress("up-to-date", 0, 0)
			}
			return previous, nil
		}
	}
	if !hasPrevious {
		if _, err := os.Stat(options.RuntimeRoot); err == nil {
			return state.Record{}, errors.New("runtime root already exists; explicitly adopt it before updating")
		} else if !errors.Is(err, os.ErrNotExist) {
			return state.Record{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(options.RuntimeRoot), 0o755); err != nil {
		return state.Record{}, err
	}
	workspace, err := os.MkdirTemp(filepath.Dir(options.RuntimeRoot), ".vasm-install-*")
	if err != nil {
		return state.Record{}, err
	}
	defer os.RemoveAll(workspace)
	archivePath := filepath.Join(workspace, asset.Name)
	if progress != nil {
		progress("download", 0, asset.Size)
	}
	if err := m.Releases.Download(ctx, options.Source, asset, archivePath, func(done, total int64) {
		if progress != nil {
			progress("download", done, total)
		}
	}); err != nil {
		return state.Record{}, err
	}
	stage := filepath.Join(workspace, "stage")
	archiveRoot := strings.TrimSuffix(assetName, target.Extension)
	if progress != nil {
		progress("extract", 0, 0)
	}
	if err := archive.Extract(archivePath, stage, archiveRoot); err != nil {
		return state.Record{}, err
	}
	if err := verifyManifest(stage, releaseInfo.Tag, target.Name, assetName); err != nil {
		return state.Record{}, err
	}
	if hasPrevious {
		if err := preserveOwnedData(options.RuntimeRoot, stage); err != nil {
			return state.Record{}, err
		}
	}
	if options.VMMEnabled != nil {
		if err := appconfig.SetScalar(stage, appconfig.Scalar{Key: "vmm_enable", Value: fmt.Sprintf("%t", *options.VMMEnabled)}); err != nil {
			return state.Record{}, err
		}
	}
	if options.VMMEndpoint != "" {
		if err := appconfig.SetScalar(stage, appconfig.Scalar{Key: "vmm", Value: options.VMMEndpoint}); err != nil {
			return state.Record{}, err
		}
	}
	if options.SkillNames != nil {
		if hasPrevious {
			return state.Record{}, errors.New("explicit skill selection is only supported for a first installation; use config skill for an existing instance")
		}
		if err := appconfig.SelectEnabledSkills(stage, options.SkillNames); err != nil {
			return state.Record{}, err
		}
	}
	current := state.Record{
		SchemaVersion:    1,
		RuntimeRoot:      options.RuntimeRoot,
		AppTag:           releaseInfo.Tag,
		Source:           options.Source.Kind,
		MirrorBase:       options.Source.MirrorBase,
		ServiceName:      "VulcanAgentService",
		ServiceScope:     options.ServiceScope,
		ServiceInstalled: options.InstallService,
		Startup:          options.Startup,
		Managed:          true,
	}
	if hasPrevious {
		current.ServiceName = previous.ServiceName
		current.ServiceScope = previous.ServiceScope
		current.ServiceInstalled = previous.ServiceInstalled
		current.Startup = previous.Startup
		current.PathAdded = previous.PathAdded
	}
	if current.ServiceScope == "" {
		current.ServiceScope = "user"
	}
	wasRunning := false
	if hasPrevious && previous.ServiceInstalled {
		wasRunning, err = service.Running(ctx, previous)
		if err != nil {
			return state.Record{}, fmt.Errorf("cannot determine the existing native service state: %w", err)
		}
	}
	if wasRunning {
		if progress != nil {
			progress("stop-service", 0, 0)
		}
		if _, err := service.Lifecycle(ctx, previous, "stop"); err != nil {
			return state.Record{}, err
		}
	}
	serviceStopped := wasRunning
	backup := filepath.Join(workspace, "backup")
	if hasPrevious {
		if err := os.Rename(options.RuntimeRoot, backup); err != nil {
			if serviceStopped {
				_, _ = service.Lifecycle(context.Background(), previous, "start")
			}
			return state.Record{}, err
		}
	}
	if err := os.Rename(stage, options.RuntimeRoot); err != nil {
		if hasPrevious {
			_ = os.Rename(backup, options.RuntimeRoot)
			if serviceStopped {
				_, _ = service.Lifecycle(context.Background(), previous, "start")
			}
		}
		return state.Record{}, err
	}
	// Keep the old tree intact until service initialization and state commit both succeed.
	// 在服务初始化与状态提交均成功前完整保留旧安装目录。
	committed := false
	newServiceRegistered := false
	defer func() {
		if committed {
			return
		}
		if newServiceRegistered {
			_, _ = service.Lifecycle(context.Background(), current, "uninstall")
		}
		_ = os.RemoveAll(options.RuntimeRoot)
		if hasPrevious {
			_ = os.Rename(backup, options.RuntimeRoot)
			if wasRunning {
				_, _ = service.Lifecycle(context.Background(), previous, "start")
			}
		}
	}()
	if options.InitializeSkills {
		if progress != nil {
			progress("initialize-skills", 0, 0)
		}
		if _, err := service.Run(ctx, options.RuntimeRoot, "init", "--runtime-root", options.RuntimeRoot); err != nil {
			return state.Record{}, err
		}
	}
	if !hasPrevious && options.InstallService {
		if progress != nil {
			progress("install-service", 0, 0)
		}
		if _, err := service.Install(ctx, current, options.Startup, false); err != nil {
			return state.Record{}, err
		}
		newServiceRegistered = true
		if options.StartService {
			if _, err := service.Lifecycle(ctx, current, "start"); err != nil {
				return state.Record{}, err
			}
		}
	} else if wasRunning {
		if _, err := service.Lifecycle(ctx, current, "start"); err != nil {
			return state.Record{}, err
		}
	}
	if err := state.Save(options.StateFile, current); err != nil {
		return state.Record{}, err
	}
	committed = true
	if progress != nil {
		progress("complete", asset.Size, asset.Size)
	}
	return current, nil
}

// verifyManifest confirms the extracted package identifies the selected app Release and binary.
// verifyManifest 确认解压包标识所选应用发布版本及二进制文件。
func verifyManifest(root, tag, platformName, archiveName string) error {
	contents, err := os.ReadFile(filepath.Join(root, "release-manifest.json"))
	if err != nil {
		return err
	}
	var manifest struct {
		SchemaVersion int    `json:"schema_version"`
		ProductName   string `json:"product_name"`
		Tag           string `json:"tag"`
		Platform      string `json:"platform"`
		Archive       string `json:"archive"`
		Contents      struct {
			Binary string `json:"binary"`
		} `json:"contents"`
		Traceability struct {
			BinarySHA256 string `json:"binary_sha256"`
		} `json:"traceability"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return err
	}
	if manifest.SchemaVersion != 1 || manifest.ProductName != "vulcan-agent-service" || manifest.Tag != tag || manifest.Platform != platformName || manifest.Archive != archiveName {
		return errors.New("release manifest does not match the selected asset")
	}
	binaryName := filepath.Base(service.Executable(root))
	if manifest.Contents.Binary != "bin/"+binaryName || len(manifest.Traceability.BinarySHA256) != 64 {
		return errors.New("release manifest has an invalid binary declaration")
	}
	binary, err := os.Open(service.Executable(root))
	if err != nil {
		return err
	}
	defer binary.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, binary); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(manifest.Traceability.BinarySHA256) {
		return errors.New("service binary does not match release manifest")
	}
	for _, name := range []string{"config.yaml", "client_budgets.yaml", "system_skills.json"} {
		if _, err := os.Stat(filepath.Join(root, "configs", name)); err != nil {
			return fmt.Errorf("release is missing %s: %w", name, err)
		}
	}
	return nil
}

// preserveOwnedData copies mutable configuration and skill state into the new staged tree.
// preserveOwnedData 将可变配置及技能状态复制到新版本暂存目录。
func preserveOwnedData(previousRoot, stagedRoot string) error {
	for _, relative := range []string{"configs", "logs", filepath.Join("lua_runtime", "skills"), filepath.Join("lua_runtime", "state")} {
		source := filepath.Join(previousRoot, relative)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyTree(source, filepath.Join(stagedRoot, relative)); err != nil {
			return err
		}
	}
	return nil
}

// copyTree copies ordinary files and directories, refusing links and special files.
// copyTree 复制普通文件及目录，拒绝链接和特殊文件。
func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular managed path %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		inputCloseErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return inputCloseErr
	})
}
