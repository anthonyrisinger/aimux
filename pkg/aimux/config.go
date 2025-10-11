package aimux

// config.go - Configuration for personas and genera

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed config.json
var defaultConfigJSON []byte

const (
	templatesDir = "templates"
	hintsDir     = "hints"
	configFile   = "config.json"
)

// PersonaConfig defines a persona's model preferences and behavioral hints.
type PersonaConfig struct {
	Name       string   `json:"name"`
	Model      string   `json:"model"`
	Model2     string   `json:"model2"`
	Hints      []string `json:"hints"`
	Delegatees []string `json:"delegatees"`
}

// GenusConfig defines an AI provider's executable, command prefix, args, and persona mappings.
type GenusConfig struct {
	Name     string                 `json:"name"`
	Exe      []string               `json:"exe"`
	Cmd      []string               `json:"cmd"`
	Args     GenusArgs              `json:"args"`
	Personas map[string]PersonaVars `json:"personas"`
}

// GenusArgs defines CLI argument templates for different session modes.
type GenusArgs struct {
	Model  []string `json:"model"`
	Resume []string `json:"resume"`
	Branch []string `json:"branch"`
	New    []string `json:"new"`
	Prompt any      `json:"prompt"`
	Output []string `json:"output"`
	Safety []string `json:"safety"`
}

// PersonaVars holds variable substitutions for flag template rendering.
type PersonaVars map[string]string

// Config holds the complete configuration with personas and genera.
type Config struct {
	Personas map[string]PersonaConfig `json:"personas"`
	Genera   map[string]GenusConfig   `json:"genera"`
}

// DefaultConfig returns built-in configuration parsed from embedded config.json.
// Returns an error if the embedded config is malformed (indicates broken build).
func DefaultConfig() (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
		return nil, fmt.Errorf("invalid embedded config.json: %w", err)
	}

	initConfigMaps(&cfg)
	return &cfg, nil
}

// initConfigMaps ensures config maps are initialized
func initConfigMaps(cfg *Config) {
	if cfg.Personas == nil {
		cfg.Personas = make(map[string]PersonaConfig)
	}
	if cfg.Genera == nil {
		cfg.Genera = make(map[string]GenusConfig)
	}
}

// LoadConfig loads configuration from ~/.aimux/config.json, merging with embedded defaults.
// Auto-generates directories and config file if missing, falling back to defaults on errors.
func LoadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		Warn("Failed to get home directory, using defaults: %v", err)
		return DefaultConfig()
	}

	cfgDir := filepath.Join(home, aimuxDir)
	configPath := filepath.Join(cfgDir, configFile)

	// Ensure ~/.aimux directory exists
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		Warn("Failed to create config directory, using defaults: %v", err)
		return DefaultConfig()
	}

	// Ensure ~/.aimux/templates directory exists
	tmplDir := filepath.Join(cfgDir, templatesDir)
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		Warn("Failed to create templates directory, using defaults: %v", err)
		return DefaultConfig()
	}

	// Auto-generate config.json if missing
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, defaultConfigJSON, 0o644); err != nil {
			Warn("Failed to create default config, using defaults: %v", err)
			return DefaultConfig()
		}
	}

	// Load config
	data, err := os.ReadFile(configPath)
	if err != nil {
		Warn("Failed to read config file, using defaults: %v", err)
		return DefaultConfig()
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config at %s: %w", configPath, err)
	}

	initConfigMaps(&cfg)

	// Merge with defaults (config file can override or extend)
	defaults, err := DefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("load default config: %w", err)
	}
	for k, v := range defaults.Personas {
		if _, exists := cfg.Personas[k]; !exists {
			cfg.Personas[k] = v
		}
	}
	for k, v := range defaults.Genera {
		if _, exists := cfg.Genera[k]; !exists {
			cfg.Genera[k] = v
		}
	}

	return &cfg, nil
}

// GetGenus returns genus configuration by name
func (c *Config) GetGenus(gen string) (GenusConfig, bool) {
	g, ok := c.Genera[gen]
	return g, ok
}

// GetGenusPersonaVars returns variable substitutions for a genus+persona combination
func (c *Config) GetGenusPersonaVars(gen, mod string) PersonaVars {
	genus, ok := c.Genera[gen]
	if !ok {
		Debug("Genus %s not found, using empty vars", gen)
		return PersonaVars{}
	}

	vars, ok := genus.Personas[mod]
	if ok {
		Debug("Found persona %s in genus %s: model=%s, model2=%s", mod, gen, vars["model"], vars["model2"])
		return vars
	}

	// Fallback: use mod directly as model name (escape hatch for direct model specification)
	// Choose model2 that differs from model (Claude rejects when model == model2)
	model2 := "sonnet"
	if mod == "sonnet" {
		model2 = "haiku"
	}
	Debug("Persona %s not found in genus %s, using mod value directly as model with model2 %s", mod, gen, model2)
	return PersonaVars{"model": mod, "model2": model2}
}

// RenderFlags performs {{variable}} substitution on flag templates
func RenderFlags(template []string, vars map[string]string) []string {
	result := make([]string, len(template))
	for i, t := range template {
		result[i] = t
		for key, val := range vars {
			result[i] = strings.ReplaceAll(result[i], "{{"+key+"}}", val)
		}
	}
	return result
}

// GetPersonaHints returns hints for a persona from config
func (cfg *Config) GetPersonaHints(persona string) []string {
	if r, ok := cfg.Personas[persona]; ok {
		return r.Hints
	}
	return []string{}
}

// LoadTemplateHints loads custom hints for a persona from ~/.aimux/templates/hints/<persona>.txt
// Returns nil if file doesn't exist
func LoadTemplateHints(persona string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	hintPath := filepath.Join(home, aimuxDir, templatesDir, hintsDir, persona+".txt")
	data, err := os.ReadFile(hintPath)
	if err != nil {
		return nil
	}

	// Split by lines, filter empty lines
	lines := strings.Split(string(data), "\n")
	hints := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			hints = append(hints, line)
		}
	}
	return hints
}

// ValidateCommand checks if a command is a known genus executable.
// This prevents arbitrary command execution.
func ValidateCommand(cmd string) error {
	cfg, err := LoadConfig()
	if err != nil {
		// Fall back to hardcoded allowlist if config can't be loaded
		allowed := map[string]bool{
			"claude": true,
			"codex":  true,
			"bash":   true,
		}
		if !allowed[cmd] {
			return fmt.Errorf("command %q not in allowlist (config unavailable)", cmd)
		}
		return nil
	}

	// Check if cmd matches any genus.Exe[0]
	for _, genus := range cfg.Genera {
		if len(genus.Exe) > 0 && genus.Exe[0] == cmd {
			return nil
		}
	}

	return fmt.Errorf("command %q not found in any genus configuration", cmd)
}
