package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillsCommandRejectsIgnoredArguments ensures a ROOT update never silently discards a skill ID.
// TestSkillsCommandRejectsIgnoredArguments 确保 ROOT 更新不会默默忽略用户填写的技能 ID。
func TestSkillsCommandRejectsIgnoredArguments(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "missing-state.json")
	for _, args := range [][]string{
		{"update", "specific-skill", "--layer", "ROOT"},
		{"list", "extra", "--layer", "USER"},
		{"install", "", "--layer", "ROOT"},
	} {
		err := skillsCommand(context.Background(), stateFile, args, io.Discard)
		if err == nil || strings.Contains(err.Error(), "missing-state.json") {
			t.Fatalf("invalid skill arguments were not rejected before state access: args=%v error=%v", args, err)
		}
	}
}
