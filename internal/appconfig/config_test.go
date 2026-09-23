package appconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestBudgetRulePreservesCommentsAndPrecedence checks an exact header rule before a wildcard.
// TestBudgetRulePreservesCommentsAndPrecedence 检查精确请求头规则位于通配规则之前且保留注释。
func TestBudgetRulePreservesCommentsAndPrecedence(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "configs")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "client_budgets.yaml")
	original := []byte("# retained user note\nformat_version: 1\ndefaults:\n  budgets:\n    tool_result:\n      bytes:\n        default: 20000\nclients:\n  - pattern: \"*\"\n    budgets:\n      tool_result:\n        bytes:\n          default: 1000\n")
	if err := os.WriteFile(file, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetToolResultBytes(root, "AgentClient", 8192); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte("# retained user note")) || bytes.Index(updated, []byte("AgentClient")) > bytes.Index(updated, []byte("pattern: \"*\"")) {
		t.Fatalf("exact client rule did not precede wildcard while preserving comments:\n%s", updated)
	}
	if err := SetToolResultBytes(root, "AgentClient", 16384); err != nil {
		t.Fatal(err)
	}
	updated, _ = os.ReadFile(file)
	if bytes.Count(updated, []byte("AgentClient")) != 1 || !bytes.Contains(updated, []byte("default: 16384")) {
		t.Fatalf("existing client rule was not updated in place:\n%s", updated)
	}
}

// TestSkillConfigPreservesUnknownFields checks edits do not discard forward-compatible JSON keys.
// TestSkillConfigPreservesUnknownFields 检查编辑不会丢弃供后续版本使用的 JSON 键。
func TestSkillConfigPreservesUnknownFields(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "configs")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "system_skills.json")
	if err := os.WriteFile(file, []byte(`{"format_version":1,"auto_install":true,"future":7,"skills":[{"name":"example","github":"owner/repo","enabled":true,"future_skill":"retained"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetSkillEnabled(root, "example", false); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte(`"future": 7`)) || !bytes.Contains(contents, []byte(`"future_skill": "retained"`)) {
		t.Fatalf("unknown fields were discarded:\n%s", contents)
	}
}

// TestSelectEnabledSkillsValidatesPackageNames verifies explicit first-install choices against the archive manifest.
// TestSelectEnabledSkillsValidatesPackageNames 验证首次安装的显式选择只接受发布包清单中的技能名称。
func TestSelectEnabledSkillsValidatesPackageNames(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "configs")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "system_skills.json")
	original := []byte(`{"format_version":1,"auto_install":true,"future":7,"skills":[{"name":"first","enabled":true,"future_skill":"retained"},{"name":"second","enabled":true}]}`)
	if err := os.WriteFile(file, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SelectEnabledSkills(root, []string{"missing"}); err == nil {
		t.Fatal("unknown skill was accepted")
	}
	unchanged, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatalf("rejected selection changed manifest: %s, %v", unchanged, err)
	}
	if err := SelectEnabledSkills(root, []string{"second"}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(file)
	if err != nil || !bytes.Contains(updated, []byte(`"future_skill": "retained"`)) {
		t.Fatalf("unknown fields were lost: %s, %v", updated, err)
	}
	if !bytes.Contains(updated, []byte(`"name": "first"`)) || !bytes.Contains(updated, []byte(`"enabled": false`)) {
		t.Fatalf("first skill not disabled: %s", updated)
	}
}
