package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Scalar describes a supported top-level service setting.
// Scalar 描述一个受支持的顶层服务配置项。
type Scalar struct {
	// Key is an exact config.yaml key.
	// Key 是 config.yaml 中的精确键名。
	Key string
	// Value is the new scalar text; booleans use true or false.
	// Value 是新的标量文本；布尔值使用 true 或 false。
	Value string
}

// supportedScalars lists only fields verified in the current service config contract.
// supportedScalars 仅列出已在当前服务配置契约中核实的字段。
var supportedScalars = map[string]string{
	"vmm_enable": "bool",
	"vmm":        "string",
	"http":       "string",
	"grpc":       "string",
}

// Show reads the three manager-facing service config files without exposing secrets.
// Show 读取面向管理器的三份服务配置文件，且不暴露密钥。
func Show(root string) (map[string]any, error) {
	configPath := filepath.Join(root, "configs", "config.yaml")
	budgetPath := filepath.Join(root, "configs", "client_budgets.yaml")
	skillsPath := filepath.Join(root, "configs", "system_skills.json")
	configNode, err := loadYAML(configPath)
	if err != nil {
		return nil, err
	}
	budgetNode, err := loadYAML(budgetPath)
	if err != nil {
		return nil, err
	}
	skills, err := os.ReadFile(skillsPath)
	if err != nil {
		return nil, err
	}
	var skillValue map[string]any
	if err := json.Unmarshal(skills, &skillValue); err != nil {
		return nil, err
	}
	result := map[string]any{"system_skills": skillValue, "client_budget_header": "Vulcan-Client-Match-Name"}
	for _, key := range []string{"vmm_enable", "vmm", "http", "grpc"} {
		if value := mappingValue(configNode, key); value != nil {
			result[key] = value.Value
		}
	}
	var budgetValue any
	if err := budgetNode.Decode(&budgetValue); err != nil {
		return nil, err
	}
	result["client_budgets"] = budgetValue
	return result, nil
}

// SetScalar updates one verified service setting while preserving YAML comments and unrelated keys.
// SetScalar 更新一项已核实的服务设置，并保留 YAML 注释和无关字段。
func SetScalar(root string, setting Scalar) error {
	kind, ok := supportedScalars[setting.Key]
	if !ok {
		return fmt.Errorf("unsupported config key %q", setting.Key)
	}
	if kind == "bool" && setting.Value != "true" && setting.Value != "false" {
		return errors.New("boolean config value must be true or false")
	}
	if kind == "string" && strings.TrimSpace(setting.Value) == "" {
		return errors.New("config value cannot be empty")
	}
	if setting.Key == "vmm" {
		endpoint, err := url.Parse(setting.Value)
		if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
			return errors.New("VMM endpoint must be an HTTP or HTTPS URL with a host")
		}
	}
	file := filepath.Join(root, "configs", "config.yaml")
	doc, err := loadYAML(file)
	if err != nil {
		return err
	}
	value := mappingValue(doc, setting.Key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return fmt.Errorf("config key %q is absent or is not a scalar", setting.Key)
	}
	value.Value = setting.Value
	if kind == "bool" {
		value.Tag = "!!bool"
	} else {
		value.Tag = "!!str"
	}
	return saveYAML(file, doc)
}

// SetToolResultBytes updates the exact client pattern or the default tool result byte budget.
// SetToolResultBytes 更新精确客户端模式或默认工具结果字节预算。
func SetToolResultBytes(root, pattern string, limit int) error {
	if limit <= 0 || limit > 10_000_000 {
		return errors.New("tool result byte limit must be between 1 and 10000000")
	}
	if pattern != "" && (len(pattern) > 100 || strings.ContainsAny(pattern, "*?\r\n\x00")) {
		return errors.New("client header value must be an exact name without wildcards or control characters")
	}
	file := filepath.Join(root, "configs", "client_budgets.yaml")
	doc, err := loadYAML(file)
	if err != nil {
		return err
	}
	var budget *yaml.Node
	var clients *yaml.Node
	selectedClientIndex := -1
	if pattern == "" {
		budget = mappingValue(doc, "defaults")
	} else {
		clients = mappingValue(doc, "clients")
		if clients == nil || clients.Kind != yaml.SequenceNode {
			return errors.New("clients budget sequence is missing")
		}
		for index, client := range clients.Content {
			name := mappingValue(client, "pattern")
			if name != nil && strings.EqualFold(name.Value, pattern) {
				if budget != nil {
					return fmt.Errorf("duplicate client pattern %q", pattern)
				}
				budget = client
				selectedClientIndex = index
			}
		}
	}
	if budget == nil {
		if pattern == "" {
			return errors.New("default client budget is not configured")
		}
		// Client rules use first-match precedence, so a new exact header value goes first.
		// 客户端规则按首个匹配项生效，因此新的精确请求头值必须置于最前。
		encodedPattern := strconv.Quote(pattern)
		var added yaml.Node
		if err := yaml.Unmarshal([]byte("- pattern: "+encodedPattern+"\n  budgets:\n    tool_result:\n      bytes:\n        default: "+strconv.Itoa(limit)+"\n"), &added); err != nil {
			return err
		}
		clients.Content = append([]*yaml.Node{added.Content[0].Content[0]}, clients.Content...)
		return saveYAML(file, doc)
	}
	if selectedClientIndex > 0 {
		// The service uses first-match rules, so an existing exact rule must precede wildcards.
		// 服务采用首个匹配规则，因此已有精确规则必须移到通配规则之前。
		selected := clients.Content[selectedClientIndex]
		clients.Content = append([]*yaml.Node{selected}, append(clients.Content[:selectedClientIndex], clients.Content[selectedClientIndex+1:]...)...)
	}
	budgets := mappingValue(budget, "budgets")
	toolResult := mappingValue(budgets, "tool_result")
	if toolResult == nil || toolResult.Kind != yaml.MappingNode {
		return errors.New("tool result budget mapping is missing")
	}
	bytesNode := mappingValue(toolResult, "bytes")
	if bytesNode == nil {
		bytesNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		toolResult.Content = append(toolResult.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "bytes"}, bytesNode)
	}
	if bytesNode.Kind != yaml.MappingNode {
		return errors.New("tool result byte budget must be a mapping")
	}
	defaultKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "default"}
	defaultValue := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(limit)}
	for index := 0; index+1 < len(bytesNode.Content); index += 2 {
		if bytesNode.Content[index].Value == "default" {
			defaultKey = bytesNode.Content[index]
			defaultValue = bytesNode.Content[index+1]
			if defaultValue.Kind != yaml.ScalarNode {
				return errors.New("tool result byte default is not a scalar")
			}
			defaultValue.Tag = "!!int"
			defaultValue.Value = strconv.Itoa(limit)
			break
		}
	}
	// A fixed byte limit must not be overridden by dynamic sources or a smaller token metric.
	// 固定字节上限不能继续被动态来源或更小的 token 指标覆盖。
	bytesNode.Content = []*yaml.Node{defaultKey, defaultValue}
	for index := 0; index+1 < len(toolResult.Content); index += 2 {
		if toolResult.Content[index].Value == "tokens" {
			toolResult.Content = append(toolResult.Content[:index], toolResult.Content[index+2:]...)
			break
		}
	}
	return saveYAML(file, doc)
}

// SetSkillEnabled changes one existing ROOT skill and the optional automatic installation switch.
// SetSkillEnabled 修改一项已有 ROOT 技能及可选的自动安装开关。
func SetSkillEnabled(root, name string, enabled bool) error {
	file := filepath.Join(root, "configs", "system_skills.json")
	contents, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return err
	}
	var version int
	if err := json.Unmarshal(document["format_version"], &version); err != nil || version != 1 {
		return errors.New("unsupported system skill config version")
	}
	if name == "@auto-install" {
		document["auto_install"] = json.RawMessage(fmt.Sprintf("%t", enabled))
	} else {
		var skills []map[string]json.RawMessage
		if err := json.Unmarshal(document["skills"], &skills); err != nil {
			return err
		}
		matches := 0
		for index := range skills {
			var skillName string
			if err := json.Unmarshal(skills[index]["name"], &skillName); err != nil {
				return err
			}
			if skillName == name {
				skills[index]["enabled"] = json.RawMessage(fmt.Sprintf("%t", enabled))
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("system skill %q must match exactly one configured entry", name)
		}
		updatedSkills, err := json.Marshal(skills)
		if err != nil {
			return err
		}
		document["skills"] = updatedSkills
	}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return saveBytes(file, append(updated, '\n'))
}

// SelectEnabledSkills applies an explicit first-install selection to the packaged ROOT skill manifest.
// SelectEnabledSkills 将首次安装的显式选择应用到发布包内的 ROOT 技能清单；空列表表示全部禁用。
func SelectEnabledSkills(root string, names []string) error {
	file := filepath.Join(root, "configs", "system_skills.json")
	contents, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return err
	}
	var version int
	if err := json.Unmarshal(document["format_version"], &version); err != nil || version != 1 {
		return errors.New("unsupported system skill config version")
	}
	var skills []map[string]json.RawMessage
	if err := json.Unmarshal(document["skills"], &skills); err != nil {
		return err
	}
	// Check the exact archive manifest before changing any enabled flag.
	// 修改启用标记前先根据已校验的发布包清单检查所有指定名称。
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" || wanted[name] {
			return fmt.Errorf("duplicate or empty system skill name %q", name)
		}
		wanted[name] = true
	}
	seen := make(map[string]bool, len(skills))
	for index := range skills {
		var name string
		if err := json.Unmarshal(skills[index]["name"], &name); err != nil || name == "" || seen[name] {
			return errors.New("system skill manifest has invalid or duplicate names")
		}
		seen[name] = true
		skills[index]["enabled"] = json.RawMessage(fmt.Sprintf("%t", wanted[name]))
	}
	for name := range wanted {
		if !seen[name] {
			return fmt.Errorf("system skill %q is not present in the selected Release", name)
		}
	}
	updatedSkills, err := json.Marshal(skills)
	if err != nil {
		return err
	}
	document["skills"] = updatedSkills
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return saveBytes(file, append(updated, '\n'))
}

// loadYAML reads one YAML document while retaining its comments and order.
// loadYAML 读取一份 YAML 文档，同时保留其注释与顺序。
func loadYAML(file string) (*yaml.Node, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(contents, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("expected a top-level YAML mapping")
	}
	return &doc, nil
}

// mappingValue returns the exact value node for a key in a YAML mapping.
// mappingValue 返回 YAML 映射中精确键对应的值节点。
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// saveYAML serializes a comment-preserving YAML node and commits it atomically.
// saveYAML 序列化保留注释的 YAML 节点并原子提交。
func saveYAML(file string, node *yaml.Node) error {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return saveBytes(file, buffer.Bytes())
}

// saveBytes writes a same-directory temporary file before replacing one config file.
// saveBytes 在替换配置文件前写入同目录暂存文件。
func saveBytes(file string, contents []byte) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".vasm-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
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
