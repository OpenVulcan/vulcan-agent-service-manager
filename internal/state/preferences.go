package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Preferences stores manager choices independently of an installed service instance.
// Preferences 独立于已安装的服务实例保存管理器自己的选择。
type Preferences struct {
	// SchemaVersion identifies the manager preference format.
	// SchemaVersion 标识管理器偏好文件的格式版本。
	SchemaVersion int `json:"schema_version"`
	// Source selects the official source or a configured mirror.
	// Source 选择官方源或已配置的镜像源。
	Source string `json:"source"`
	// MirrorBase is the optional HTTPS GitHub proxy prefix.
	// MirrorBase 是可选的 HTTPS GitHub 代理前缀。
	MirrorBase string `json:"mirror_base,omitempty"`
	// PathDirectory is the exact manager directory added to user PATH.
	// PathDirectory 是由管理器加入用户 PATH 的确切目录。
	PathDirectory string `json:"path_directory,omitempty"`
	// PathAdded records whether vasm owns the PATH entry.
	// PathAdded 记录 vasm 是否持有该 PATH 条目。
	PathAdded bool `json:"path_added,omitempty"`
}

// PreferencesFile returns the stable manager preference file next to service state.
// PreferencesFile 返回位于服务状态文件旁的稳定管理器偏好文件，参数为状态文件路径。
func PreferencesFile(stateFile string) string {
	return filepath.Join(filepath.Dir(stateFile), "preferences.json")
}

// LoadPreferences reads manager choices or returns official defaults when no file exists.
// LoadPreferences 读取管理器选择；文件不存在时返回官方源默认值。
func LoadPreferences(stateFile string) (Preferences, error) {
	contents, err := os.ReadFile(PreferencesFile(stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return Preferences{SchemaVersion: 1, Source: "github"}, nil
	}
	if err != nil {
		return Preferences{}, err
	}
	var preferences Preferences
	if err := json.Unmarshal(contents, &preferences); err != nil {
		return Preferences{}, err
	}
	if preferences.SchemaVersion != 1 || (preferences.Source != "github" && preferences.Source != "mirror") || (preferences.PathAdded && !filepath.IsAbs(preferences.PathDirectory)) {
		return Preferences{}, errors.New("unsupported or invalid manager preferences")
	}
	return preferences, nil
}

// SavePreferences atomically commits manager choices without changing service installation state.
// SavePreferences 原子提交管理器选择，不改变服务安装状态。
func SavePreferences(stateFile string, preferences Preferences) error {
	if preferences.SchemaVersion != 1 || (preferences.Source != "github" && preferences.Source != "mirror") || (preferences.PathAdded && !filepath.IsAbs(preferences.PathDirectory)) {
		return errors.New("invalid manager preferences")
	}
	file := PreferencesFile(stateFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".preferences-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(append(contents, '\n')); err != nil {
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
