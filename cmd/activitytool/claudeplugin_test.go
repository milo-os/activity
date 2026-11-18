package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const claudePluginDir = "../../claude-plugin"

type claudePluginSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func TestClaudePluginSkillsValid(t *testing.T) {
	skillsDir := filepath.Join(claudePluginDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}

	requiredSkills := map[string]bool{
		"author-policy":  false,
		"preview-policy": false,
		"test-policy":    false,
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		b, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("plugin skill %s missing SKILL.md: %v", e.Name(), err)
		}

		parts := strings.SplitN(string(b), "---\n", 3)
		if len(parts) != 3 {
			t.Fatalf("plugin skill %s missing YAML frontmatter", e.Name())
		}

		var sk claudePluginSkill
		if err := yaml.Unmarshal([]byte(parts[1]), &sk); err != nil {
			t.Fatalf("plugin skill %s invalid frontmatter: %v", e.Name(), err)
		}
		if sk.Name != e.Name() {
			t.Errorf("plugin skill %s frontmatter name = %q, want %q", e.Name(), sk.Name, e.Name())
		}
		if strings.TrimSpace(sk.Description) == "" {
			t.Errorf("plugin skill %s has empty description", e.Name())
		}
		if _, wanted := requiredSkills[e.Name()]; wanted {
			requiredSkills[e.Name()] = true
		}
	}

	for name, seen := range requiredSkills {
		if !seen {
			t.Errorf("required skill %q not found in %s", name, skillsDir)
		}
	}
}

func TestClaudePluginHooksAndManifestValid(t *testing.T) {
	manifestBytes, err := os.ReadFile(filepath.Join(claudePluginDir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("invalid plugin.json: %v", err)
	}

	hooksBytes, err := os.ReadFile(filepath.Join(claudePluginDir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(hooksBytes, &hooks); err != nil {
		t.Fatalf("invalid hooks.json: %v", err)
	}

	hookScript := filepath.Join(claudePluginDir, "hooks", "check-tools.sh")
	info, err := os.Stat(hookScript)
	if err != nil {
		t.Fatalf("stat check-tools.sh: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("check-tools.sh must be executable (mode=%o)", info.Mode().Perm())
	}
}
