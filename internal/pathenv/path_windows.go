//go:build windows

package pathenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Add adds a manager directory to the current user's Windows PATH registry value.
// Add 将管理器目录加入当前用户的 Windows PATH 注册表值。
func Add(directory string) (bool, error) {
	if !filepath.IsAbs(directory) {
		return false, errors.New("PATH entry must be absolute")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	current, valueType, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	for _, entry := range strings.Split(current, ";") {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(directory)) {
			return false, nil
		}
	}
	updated := strings.TrimRight(current, ";")
	if updated != "" {
		updated += ";"
	}
	updated += directory
	if len(updated) > 32767 {
		return false, fmt.Errorf("user PATH exceeds Windows length limit")
	}
	if valueType == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", updated)
	} else {
		err = key.SetStringValue("Path", updated)
	}
	return err == nil, err
}

// Remove deletes only one manager-owned directory entry from the user PATH.
// Remove 仅从用户 PATH 中删除一个由管理器加入的目录项。
func Remove(directory string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, valueType, err := key.GetStringValue("Path")
	if err != nil {
		return err
	}
	entries := strings.Split(current, ";")
	kept := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.EqualFold(filepath.Clean(entry), filepath.Clean(directory)) {
			kept = append(kept, entry)
		}
	}
	updated := strings.Join(kept, ";")
	if valueType == registry.EXPAND_SZ {
		return key.SetExpandStringValue("Path", updated)
	}
	return key.SetStringValue("Path", updated)
}
