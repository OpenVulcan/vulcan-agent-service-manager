package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/platform"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/service"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
)

// Uninstall removes a managed service package while keeping user configuration unless purge is explicit.
// Uninstall 删除受管服务程序；除非明确选择彻底清除，否则保留用户配置和管理器自身设置。
func Uninstall(ctx context.Context, stateFile string, purge bool) error {
	unlock, err := state.Lock(stateFile)
	if err != nil {
		return err
	}
	defer unlock()
	record, err := state.Load(stateFile)
	if err != nil {
		return err
	}
	target, err := platform.Current()
	if err != nil {
		return err
	}
	if err := verifyManifest(record.RuntimeRoot, record.AppTag, target.Name, platform.AppAssetName(record.AppTag, target)); err != nil {
		return fmt.Errorf("refusing to uninstall unverified managed root: %w", err)
	}
	if purge {
		if err := validatePurgeRoot(record.RuntimeRoot); err != nil {
			return err
		}
	}
	if record.ServiceInstalled {
		if _, err := service.Lifecycle(ctx, record, "uninstall"); err != nil {
			return err
		}
		record.ServiceInstalled = false
		if err := state.Save(stateFile, record); err != nil {
			return err
		}
	}
	if purge {
		if err := os.RemoveAll(record.RuntimeRoot); err != nil {
			return err
		}
	} else {
		if err := removePackagedFiles(record.RuntimeRoot); err != nil {
			return err
		}
	}
	return os.Remove(stateFile)
}

// validatePurgeRoot prevents a corrupted manager record from deleting a drive or home root.
// validatePurgeRoot 防止损坏的管理器记录删除磁盘或用户主目录根。
func validatePurgeRoot(root string) error {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) || filepath.Dir(clean) == clean || filepath.Dir(filepath.Dir(clean)) == filepath.Dir(clean) {
		return errors.New("refusing to purge a filesystem root or its direct child")
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.EqualFold(clean, filepath.Clean(home)) {
		return errors.New("refusing to purge the user home directory")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to purge a non-directory or linked runtime root")
	}
	return nil
}

// removePackagedFiles deletes only paths known to be part of the current Release layout.
// removePackagedFiles 仅删除已知属于当前发布布局的路径。
func removePackagedFiles(root string) error {
	contents, err := os.ReadFile(filepath.Join(root, "release-manifest.json"))
	if err != nil {
		return err
	}
	var manifest struct {
		Contents struct {
			Binary     string   `json:"binary"`
			WindowsCRT []string `json:"windows_crt"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return err
	}
	owned := []string{"LICENSE", "README.md", manifest.Contents.Binary}
	for _, relative := range manifest.Contents.WindowsCRT {
		clean := filepath.ToSlash(filepath.Clean(relative))
		if !strings.HasPrefix(clean, "bin/") || strings.Contains(clean, "../") || !strings.HasSuffix(strings.ToLower(clean), ".dll") {
			return errors.New("release manifest contains an unsafe CRT path")
		}
	}
	owned = append(owned, manifest.Contents.WindowsCRT...)
	for _, directory := range []string{"libs", "lua_packages", "resources", "licenses", "dependencies", "bin"} {
		owned = append(owned, filepath.Join("lua_runtime", directory))
	}
	owned = append(owned, "release-manifest.json")
	for _, relative := range owned {
		if err := validateOwnedPath(root, relative); err != nil {
			return err
		}
	}
	for _, relative := range owned {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

// validateOwnedPath rejects paths whose parent traverses outside or through a link.
// validateOwnedPath 拒绝越界路径或父目录中的链接，参数为运行根和相对发布路径。
func validateOwnedPath(root, relative string) error {
	local := filepath.FromSlash(relative)
	if !filepath.IsLocal(local) {
		return errors.New("release manifest contains an unsafe owned path")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("release root is not an ordinary directory")
	}
	current := root
	parts := strings.Split(filepath.Clean(local), string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("release-owned path traverses a linked or non-directory parent: %s", current)
		}
	}
	return nil
}
