//go:build !windows

package pathenv

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// markerStart identifies a profile block written only by vasm.
// markerStart 标识仅由 vasm 写入的 shell 配置块起点。
const markerStart = "# vasm PATH begin\n"

// markerEnd identifies the end of a vasm-owned shell profile block.
// markerEnd 标识由 vasm 拥有的 shell 配置块终点。
const markerEnd = "# vasm PATH end\n"

// profile returns the login profile for the user's shell.
// profile 返回用户 shell 的登录配置文件。
func profile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(os.Getenv("SHELL"), "/zsh") {
		return filepath.Join(home, ".zprofile"), nil
	}
	return filepath.Join(home, ".profile"), nil
}

// block returns a shell-safe PATH entry wrapped in an ownership marker.
// block 返回带所有权标记且经 shell 安全引用的 PATH 条目。
func block(directory string) []byte {
	quoted := "'" + strings.ReplaceAll(directory, "'", "'\\''") + "'"
	return []byte(markerStart + "export PATH=\"$PATH\":" + quoted + "\n" + markerEnd)
}

// Add appends one idempotent manager-owned block to the user shell profile.
// Add 向用户 shell 配置文件追加一块幂等的管理器专有内容。
func Add(directory string) (bool, error) {
	if !filepath.IsAbs(directory) {
		return false, errors.New("PATH entry must be absolute")
	}
	file, err := profile()
	if err != nil {
		return false, err
	}
	contents, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if bytes.Contains(contents, []byte(markerStart)) {
		return false, nil
	}
	if len(contents) > 0 && contents[len(contents)-1] != '\n' {
		contents = append(contents, '\n')
	}
	contents = append(contents, block(directory)...)
	if err := saveProfile(file, contents); err != nil {
		return false, err
	}
	return true, nil
}

// Remove deletes only the exact manager-owned PATH block from the shell profile.
// Remove 仅从 shell 配置文件中删除精确匹配的管理器 PATH 块。
func Remove(directory string) error {
	file, err := profile()
	if err != nil {
		return err
	}
	owned := block(directory)
	// A user may change login shells after adding the entry; inspect both supported profiles.
	// 用户添加条目后可能更换登录 shell，因此检查两种受支持的配置文件。
	files := []string{file}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, candidate := range []string{filepath.Join(home, ".profile"), filepath.Join(home, ".zprofile")} {
		if candidate != file {
			files = append(files, candidate)
		}
	}
	for _, candidate := range files {
		contents, err := os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if bytes.Contains(contents, owned) {
			if err := saveProfile(candidate, bytes.Replace(contents, owned, nil, 1)); err != nil {
				return err
			}
		}
	}
	return nil
}

// saveProfile atomically replaces a user shell profile while preserving its permissions.
// saveProfile 原子替换用户 shell 配置文件，同时保留其权限。
func saveProfile(file string, contents []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Lstat(file); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refusing to replace a linked or non-regular shell profile")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".vasm-profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(contents); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), file)
}
