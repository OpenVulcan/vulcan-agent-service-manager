package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/archive"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/platform"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
)

// Version is injected by the independent manager release workflow.
// Version 由独立的管理器发布工作流注入。
var Version = "0.1.1"

// Candidate is the latest manager Release resolved from GitHub metadata.
// Candidate 是从 GitHub 元数据解析出的最新管理器发布版本。
type Candidate struct {
	// Tag is the published release tag.
	// Tag 是已发布的版本标签。
	Tag string
	// Asset is the exact archive for the running platform.
	// Asset 是当前平台对应的精确归档。
	Asset release.Asset
}

// Result records the last attempted manager self-update for later diagnostics.
// Result 记录最近一次管理器自身更新，以供后续诊断。
type Result struct {
	// Tag is the requested manager Release tag.
	// Tag 是请求更新的管理器发布标签。
	Tag string `json:"tag"`
	// Status is pending, success, or failed.
	// Status 为 pending、success 或 failed。
	Status string `json:"status"`
	// Error is an optional failure explanation.
	// Error 是可选的失败说明。
	Error string `json:"error,omitempty"`
	// UpdatedAt is the UTC time of the latest status write.
	// UpdatedAt 是最近一次状态写入的 UTC 时间。
	UpdatedAt string `json:"updated_at"`
}

// Check resolves the latest manager release and its platform asset.
// Check 解析管理器最新发布版本及其平台资产。
func Check(ctx context.Context, client *release.Client, tag string) (Candidate, error) {
	info, err := client.Fetch(ctx, release.ManagerRepository, tag)
	if err != nil {
		return Candidate{}, err
	}
	target, err := platform.Current()
	if err != nil {
		return Candidate{}, err
	}
	asset, err := info.FindAsset(platform.ManagerAssetName(target))
	if err != nil {
		return Candidate{}, err
	}
	return Candidate{Tag: info.Tag, Asset: asset}, nil
}

// Compare compares two validated numeric major.minor.patch release tags.
// Compare 比较两个经校验的主版本、次版本和修订版本标签。
func Compare(left, right string) (int, error) {
	return release.CompareTags(left, right)
}

// Update downloads and verifies a manager archive before installing its executable.
// Update 在安装管理器可执行文件前下载并校验管理器归档。
func Update(ctx context.Context, client *release.Client, source release.Source, tag string) (string, error) {
	candidate, err := Check(ctx, client, tag)
	if err != nil {
		return "", err
	}
	currentTag := "v" + strings.TrimPrefix(Version, "v")
	comparison, err := Compare(candidate.Tag, currentTag)
	if err != nil {
		return "", err
	}
	if comparison < 0 {
		return "", errors.New("selected manager Release is older than the running version")
	}
	if comparison == 0 {
		return candidate.Tag, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	target, err := platform.Current()
	if err != nil {
		return "", err
	}
	workspace, err := os.MkdirTemp(filepath.Dir(executable), ".vasm-update-*")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(workspace, ".owner"), []byte(executable), 0o600); err != nil {
		_ = os.RemoveAll(workspace)
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(workspace)
		}
	}()
	archivePath := filepath.Join(workspace, candidate.Asset.Name)
	if err := client.Download(ctx, source, candidate.Asset, archivePath, nil); err != nil {
		return "", err
	}
	stage := filepath.Join(workspace, "stage")
	if err := archive.Extract(archivePath, stage, strings.TrimSuffix(candidate.Asset.Name, target.Extension)); err != nil {
		return "", err
	}
	newBinary := filepath.Join(stage, target.Executable)
	info, err := os.Stat(newBinary)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("manager archive does not contain the expected executable")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkCtx, newBinary, "--version").Output()
	if err != nil || strings.TrimSpace(string(output)) != "vasm "+strings.TrimPrefix(candidate.Tag, "v") {
		return "", errors.New("downloaded manager binary failed its version smoke test")
	}
	backup := filepath.Join(workspace, "previous"+filepath.Ext(executable))
	if runtime.GOOS == "windows" {
		// The staged new binary acts as a helper after the running old binary exits.
		// 旧版进程退出后，由暂存的新二进制作为替换辅助进程。
		if err := saveResult(executable, Result{Tag: candidate.Tag, Status: "pending"}); err != nil {
			return "", err
		}
		command := exec.Command(newBinary, "--internal-apply-self-update", strconv.Itoa(os.Getpid()), executable, backup, newBinary, candidate.Tag)
		if err := command.Start(); err != nil {
			_ = saveResult(executable, Result{Tag: candidate.Tag, Status: "failed", Error: err.Error()})
			return "", err
		}
		cleanup = false
		return candidate.Tag, nil
	}
	if err := os.Rename(executable, backup); err != nil {
		return "", err
	}
	if err := copyExecutable(newBinary, executable); err != nil {
		_ = os.Rename(backup, executable)
		return "", err
	}
	_ = saveResult(executable, Result{Tag: candidate.Tag, Status: "success"})
	return candidate.Tag, nil
}

// CleanupStale removes only completed manager-owned self-update workspaces older than one hour.
// CleanupStale 仅移除超过一小时且已完成的管理器自更新暂存目录。
func CleanupStale(executable string) {
	if receipt, err := LastResult(executable); err == nil && receipt.Status == "pending" {
		return
	}
	parent := filepath.Dir(executable)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".vasm-update-") {
			continue
		}
		workspace := filepath.Join(parent, entry.Name())
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		owner, err := os.ReadFile(filepath.Join(workspace, ".owner"))
		if err != nil || filepath.Clean(string(owner)) != filepath.Clean(executable) {
			continue
		}
		_ = os.RemoveAll(workspace)
	}
}

// ApplyWindows waits for the old process to exit, swaps binaries, and restores on failure.
// ApplyWindows 等待旧进程退出、替换二进制文件，并在失败时恢复。
func ApplyWindows(pid int, destination, backup, source, tag string) (resultErr error) {
	if runtime.GOOS != "windows" || pid <= 0 || filepath.Base(destination) != "vasm.exe" || filepath.Base(source) != "vasm.exe" || release.ValidateTag(tag) != nil {
		return errors.New("invalid self-update helper invocation")
	}
	// The helper may only replace the sibling manager that created its own staging directory.
	// 辅助进程只能替换创建其暂存目录的同级管理器，避免内部命令写入任意路径。
	currentExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	workspace := filepath.Dir(filepath.Dir(source))
	// Windows can give the same path in 8.3 and long-name forms, so compare filesystem identities.
	// Windows 可用 8.3 短路径和长路径表示同一位置，因此比较文件系统身份。
	if !filepath.IsAbs(destination) || !filepath.IsAbs(source) || !filepath.IsAbs(backup) || !strings.HasPrefix(filepath.Base(workspace), ".vasm-update-") || !sameExistingFile(currentExecutable, source) || !sameExistingFile(filepath.Dir(workspace), filepath.Dir(destination)) || filepath.Clean(backup) != filepath.Join(workspace, "previous.exe") || filepath.Clean(source) != filepath.Join(workspace, "stage", "vasm.exe") {
		return errors.New("self-update helper paths do not match the staged manager")
	}
	defer func() {
		status := Result{Tag: tag, Status: "success"}
		if resultErr != nil {
			status.Status = "failed"
			status.Error = resultErr.Error()
		}
		_ = saveResult(destination, status)
	}()
	deadline := time.Now().Add(5 * time.Minute)
	for {
		err := os.Rename(destination, backup)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("previous manager did not exit: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := copyExecutable(source, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return fmt.Errorf("replacement failed: %w; backup restore failed: %v", err, restoreErr)
		}
		return err
	}
	output, err := exec.Command(destination, "--version").Output()
	if err != nil || strings.TrimSpace(string(output)) != "vasm "+strings.TrimPrefix(tag, "v") {
		if removeErr := os.Remove(destination); removeErr != nil {
			return fmt.Errorf("updated manager failed its launch smoke test and could not be removed: %w", removeErr)
		}
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return fmt.Errorf("updated manager failed its launch smoke test and backup restore failed: %w", restoreErr)
		}
		return errors.New("updated manager failed its launch smoke test")
	}
	_ = os.Remove(backup)
	return nil
}

// sameExistingFile compares two existing filesystem objects across Windows short and long path spellings.
// sameExistingFile 比较两个现有文件系统对象，兼容 Windows 短路径和长路径写法。
func sameExistingFile(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

// LastResult reads the most recent self-update receipt next to a manager executable.
// LastResult 读取管理器可执行文件旁最近一次自身更新回执。
func LastResult(executable string) (Result, error) {
	contents, err := os.ReadFile(executable + ".update-status.json")
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := json.Unmarshal(contents, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// saveResult atomically writes one self-update receipt next to the manager executable.
// saveResult 在管理器可执行文件旁原子写入一次自身更新回执。
func saveResult(executable string, result Result) error {
	result.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(executable), ".vasm-update-status-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := temp.Write(append(contents, '\n')); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), executable+".update-status.json")
}

// copyExecutable writes a new executable through a sibling temporary file before renaming it.
// copyExecutable 先写入同目录临时文件，再将新可执行文件更名到目标路径。
func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".vasm-binary-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := io.Copy(temp, input); err != nil {
		return err
	}
	if err := temp.Chmod(0o755); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), destination)
}
