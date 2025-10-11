package aimux

import (
	"testing"
)

func TestRenderFlags(t *testing.T) {
	template := []string{"--model", "{{model}}", "--fallback-model", "{{model2}}"}
	vars := map[string]string{"model": "opus", "model2": "sonnet"}

	result := RenderFlags(template, vars)

	expected := []string{"--model", "opus", "--fallback-model", "sonnet"}
	if len(result) != len(expected) {
		t.Fatalf("Expected %d flags, got %d", len(expected), len(result))
	}

	for i, val := range expected {
		if result[i] != val {
			t.Errorf("Flag %d: expected %q, got %q", i, val, result[i])
		}
	}
}

func TestGetGenus(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() failed: %v", err)
	}

	// Test Claude genus exists
	claude, ok := cfg.GetGenus("claude")
	if !ok {
		t.Fatal("Claude genus not found")
	}
	if claude.Name != "claude" {
		t.Errorf("Expected name 'claude', got %q", claude.Name)
	}
	if len(claude.Exe) == 0 || claude.Exe[0] != "claude" {
		t.Errorf("Expected exe[0] 'claude', got %v", claude.Exe)
	}

	// Test Codex genus exists
	codex, ok := cfg.GetGenus("codex")
	if !ok {
		t.Fatal("Codex genus not found")
	}
	if codex.Name != "codex" {
		t.Errorf("Expected name 'codex', got %q", codex.Name)
	}

	// Test invalid genus
	_, ok = cfg.GetGenus("invalid")
	if ok {
		t.Error("Expected invalid genus to not exist")
	}
}

func TestGetGenusPersonaVars(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() failed: %v", err)
	}

	// Test undifferentiated claude
	vars := cfg.GetGenusPersonaVars("claude", "")
	if vars["model"] != "sonnet" {
		t.Errorf("Expected model 'sonnet', got %q", vars["model"])
	}
	if vars["model2"] != "opusplan" {
		t.Errorf("Expected model2 'opusplan', got %q", vars["model2"])
	}

	// Test architect persona
	vars = cfg.GetGenusPersonaVars("claude", "architect")
	if vars["model"] != "opus" {
		t.Errorf("Expected model 'opus', got %q", vars["model"])
	}

	// Test codex with effort
	vars = cfg.GetGenusPersonaVars("codex", "")
	if vars["model"] != "gpt-5-codex" {
		t.Errorf("Expected model 'gpt-5-codex', got %q", vars["model"])
	}
	if vars["effort"] != "medium" {
		t.Errorf("Expected effort 'medium', got %q", vars["effort"])
	}

	// Test missing persona (should use mod as model name with fallback)
	vars = cfg.GetGenusPersonaVars("claude", "nonexistent")
	if vars["model"] != "nonexistent" {
		t.Errorf("Expected model 'nonexistent', got %q", vars["model"])
	}
	if vars["model2"] != "sonnet" {
		t.Errorf("Expected model2 'sonnet', got %q", vars["model2"])
	}
}

func TestGenusArgs(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() failed: %v", err)
	}
	claude, _ := cfg.GetGenus("claude")

	// Test model args
	if len(claude.Args.Model) == 0 {
		t.Error("Expected model args to be defined")
	}

	// Test session args
	if len(claude.Args.Resume) == 0 {
		t.Error("Expected resume args to be defined")
	}
	if len(claude.Args.Branch) == 0 {
		t.Error("Expected branch args to be defined")
	}
	if len(claude.Args.New) == 0 {
		t.Error("Expected new args to be defined")
	}

	// Test prompt
	if claude.Args.Prompt == nil {
		t.Error("Expected prompt to be defined")
	}

	// Test codex stdin handling
	codex, _ := cfg.GetGenus("codex")
	if sp, ok := codex.Args.Prompt.(string); !ok || sp != "stdin" {
		t.Error("Expected codex prompt to be 'stdin'")
	}
}
