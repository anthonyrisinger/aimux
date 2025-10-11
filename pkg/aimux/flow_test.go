package aimux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSessions verifies session creation and resumption
func TestSessions(t *testing.T) {
	// Create temporary test directory
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	t.Run("InitContext creates new session", func(t *testing.T) {
		ctx, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		if ctx.CID == "" {
			t.Error("InitContext() CID is empty")
		}

		if ctx.SID != ctx.CID {
			t.Errorf("InitContext() SID = %v, want %v", ctx.SID, ctx.CID)
		}

		if ctx.TAG != "bash" {
			t.Errorf("InitContext() TAG = %v, want bash", ctx.TAG)
		}

		// Directory should NOT be created by InitContext (delayed until first output)
		dir, _ := Dir2(ctx)
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("InitContext() should not create directory %v (delayed until first output)", dir)
		}
	})

	t.Run("ResumeContext loads existing session", func(t *testing.T) {
		// First create a session
		ctx1, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		// Create the directory and context.json file to simulate a persisted conversation
		if err := os.MkdirAll(ctx1.DIR, 0755); err != nil {
			t.Fatalf("mkdir error = %v", err)
		}
		if err := saveContext(ctx1); err != nil {
			t.Fatalf("saveContext error = %v", err)
		}
		defer os.RemoveAll(ctx1.DIR)

		// Resume the session
		ctx2, err := ResumeContext(ctx1.CID, "bash", "")
		if err != nil {
			t.Fatalf("ResumeContext() error = %v", err)
		}

		if ctx2.CID != ctx1.CID {
			t.Errorf("ResumeContext() CID = %v, want %v", ctx2.CID, ctx1.CID)
		}
	})

	t.Run("Branch creates new session ID", func(t *testing.T) {
		ctx, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		originalSID := ctx.SID

		err = Branch(ctx)
		if err != nil {
			t.Fatalf("Branch() error = %v", err)
		}

		if ctx.SID == originalSID {
			t.Error("Branch() did not change SID")
		}

		if ctx.CID != originalSID {
			t.Error("Branch() changed CID")
		}
	})
}

// TestBuildSessionFlags verifies flag construction logic for session management
func TestBuildSessionFlags(t *testing.T) {
	// Create temporary test directory
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create test genus config
	testGenus := GenusConfig{
		Name: "test",
		Exe:  []string{"test"},
		Cmd:  []string{},
		Args: GenusArgs{
			Resume: []string{"--resume", "{{sid}}"},
			Branch: []string{"--resume", "{{sid}}", "--fork-session"},
			New:    []string{"--session-id", "{{sid}}"},
		},
	}

	personaVars := PersonaVars{"model": "test"}

	// Helper to create a log file with assistant messages (established session)
	createEstablishedLog := func(path string, sid ID) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		msg := map[string]interface{}{
			"session_id": sid,
			"from":       "assistant",
			"body":       "test response",
		}
		data, _ := json.Marshal(msg)
		return os.WriteFile(path, append(data, '\n'), 0o644)
	}

	// Helper to create an empty log file
	createEmptyLog := func(path string) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte{}, 0o644)
	}

	t.Run("log2 has established session returns Resume flags", func(t *testing.T) {
		ctx, err := InitContext("bash", "architect")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		log2, _ := Log2(ctx)
		if err := createEstablishedLog(log2, ctx.SID); err != nil {
			t.Fatalf("Failed to create log: %v", err)
		}

		flags, _, err := buildSessionFlags(ctx, testGenus, personaVars)
		if err != nil {
			t.Fatalf("buildSessionFlags() error = %v", err)
		}

		expected := []string{"--resume", string(ctx.SID)}
		if len(flags) != len(expected) {
			t.Errorf("buildSessionFlags() flags = %v, want %v", flags, expected)
		}
		for i, f := range expected {
			if i >= len(flags) || flags[i] != f {
				t.Errorf("buildSessionFlags() flags[%d] = %v, want %v", i, flags[i], f)
			}
		}
	})

	t.Run("log1 established, log2 exists with Branch flags returns Branch", func(t *testing.T) {
		ctx, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		log1, _ := Log1(ctx)
		if err := createEstablishedLog(log1, ctx.SID); err != nil {
			t.Fatalf("Failed to create log1: %v", err)
		}

		// Change to architect persona (creates log2 path)
		ctx.MOD = "architect"
		ctx.TAG = Tag3(ctx)
		dir, _ := Dir2(ctx)
		ctx.DIR = dir

		log2, _ := Log2(ctx)
		if err := createEmptyLog(log2); err != nil {
			t.Fatalf("Failed to create log2: %v", err)
		}

		flags, _, err := buildSessionFlags(ctx, testGenus, personaVars)
		if err != nil {
			t.Fatalf("buildSessionFlags() error = %v", err)
		}

		expected := []string{"--resume", string(ctx.SID), "--fork-session"}
		if len(flags) != len(expected) {
			t.Errorf("buildSessionFlags() flags = %v, want %v", flags, expected)
		}
		for i, f := range expected {
			if i >= len(flags) || flags[i] != f {
				t.Errorf("buildSessionFlags() flags[%d] = %v, want %v", i, flags[i], f)
			}
		}
	})

	t.Run("log1 established, log2 exists, no Branch flags returns Resume", func(t *testing.T) {
		ctx, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		log1, _ := Log1(ctx)
		if err := createEstablishedLog(log1, ctx.SID); err != nil {
			t.Fatalf("Failed to create log1: %v", err)
		}

		// Change to architect persona
		ctx.MOD = "architect"
		ctx.TAG = Tag3(ctx)
		dir, _ := Dir2(ctx)
		ctx.DIR = dir

		log2, _ := Log2(ctx)
		if err := createEmptyLog(log2); err != nil {
			t.Fatalf("Failed to create log2: %v", err)
		}

		// Use genus without Branch args
		genusNoBranch := testGenus
		genusNoBranch.Args.Branch = []string{}

		flags, _, err := buildSessionFlags(ctx, genusNoBranch, personaVars)
		if err != nil {
			t.Fatalf("buildSessionFlags() error = %v", err)
		}

		expected := []string{"--resume", string(ctx.SID)}
		if len(flags) != len(expected) {
			t.Errorf("buildSessionFlags() flags = %v, want %v", flags, expected)
		}
	})

	t.Run("log1 established, log2 doesn't exist returns Resume", func(t *testing.T) {
		ctx, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		log1, _ := Log1(ctx)
		if err := createEstablishedLog(log1, ctx.SID); err != nil {
			t.Fatalf("Failed to create log1: %v", err)
		}

		flags, _, err := buildSessionFlags(ctx, testGenus, personaVars)
		if err != nil {
			t.Fatalf("buildSessionFlags() error = %v", err)
		}

		expected := []string{"--resume", string(ctx.SID)}
		if len(flags) != len(expected) {
			t.Errorf("buildSessionFlags() flags = %v, want %v", flags, expected)
		}
	})

	// Removed test: behavior changed to preserve CID=SID for fresh conversations

	t.Run("no established sessions, log2 doesn't exist uses existing SID", func(t *testing.T) {
		ctx, err := InitContext("bash", "")
		if err != nil {
			t.Fatalf("InitContext() error = %v", err)
		}

		originalSID := ctx.SID

		flags, _, err := buildSessionFlags(ctx, testGenus, personaVars)
		if err != nil {
			t.Fatalf("buildSessionFlags() error = %v", err)
		}

		// SID should not have changed
		if ctx.SID != originalSID {
			t.Error("buildSessionFlags() changed SID for fresh start")
		}

		expected := []string{"--session-id", string(ctx.SID)}
		if len(flags) != len(expected) {
			t.Errorf("buildSessionFlags() flags = %v, want %v", flags, expected)
		}
	})
}

func TestInferFlowHints(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected map[string]string
	}{
		{
			name:   "exploration phase - high question density",
			prompt: "What caching strategies exist? How do they compare? Which is best?",
			expected: map[string]string{
				"PHASE_HINT": "explore",
			},
		},
		{
			name:   "design phase - design keyword",
			prompt: "Design a caching architecture with Redis",
			expected: map[string]string{
				"PHASE_HINT": "design",
			},
		},
		{
			name:   "design phase - architect keyword",
			prompt: "Architect a scalable OAuth2 system",
			expected: map[string]string{
				"PHASE_HINT": "design",
			},
		},
		{
			name:   "implement phase - implement keyword",
			prompt: "Implement the Redis caching layer",
			expected: map[string]string{
				"PHASE_HINT": "implement",
			},
		},
		{
			name:   "implement phase - build keyword",
			prompt: "Build the authentication handler",
			expected: map[string]string{
				"PHASE_HINT": "implement",
			},
		},
		{
			name:   "test phase - test keyword",
			prompt: "Test the OAuth2 flow with various edge cases",
			expected: map[string]string{
				"PHASE_HINT": "test",
			},
		},
		{
			name:   "review phase - review keyword",
			prompt: "Review the implementation and suggest improvements",
			expected: map[string]string{
				"PHASE_HINT": "review",
			},
		},
		{
			name:   "high temperature - multiple bold emphasis",
			prompt: "**Design** a **scalable** caching system",
			expected: map[string]string{
				"PHASE_HINT": "design",
				"TEMP_HINT":  "high",
			},
		},
		{
			name:   "medium temperature - single bold emphasis",
			prompt: "**Design** the caching system",
			expected: map[string]string{
				"PHASE_HINT": "design",
				"TEMP_HINT":  "medium",
			},
		},
		{
			name:   "medium temperature - italic emphasis",
			prompt: "*Carefully consider* the tradeoffs in caching strategies",
			expected: map[string]string{
				"TEMP_HINT": "medium",
			},
		},
		{
			name:   "CID reference - from CID pattern",
			prompt: "Based on the exploration from CID abc-123-def, implement Redis",
			expected: map[string]string{
				"PHASE_HINT": "implement",
				"REF_CID":    "abc-123-def",
			},
		},
		{
			name:   "CID reference - bracket pattern",
			prompt: "Refer to conversation [CID: xyz-789] for requirements",
			expected: map[string]string{
				"REF_CID": "xyz-789",
			},
		},
		{
			name:   "goal extraction - I want to",
			prompt: "I want to build a distributed tracing system",
			expected: map[string]string{
				"PHASE_HINT": "implement", // "build" triggers implement phase
				"GOAL_HINT":  "build a distributed tracing system",
			},
		},
		{
			name:   "goal extraction - Let's",
			prompt: "Let's create an OAuth2 authentication provider",
			expected: map[string]string{
				"PHASE_HINT": "implement", // "create" triggers implement phase
				"GOAL_HINT":  "create an OAuth2 authentication provider",
			},
		},
		{
			name:   "goal extraction - Goal:",
			prompt: "Goal: Implement Redis caching with write-through consistency",
			expected: map[string]string{
				"PHASE_HINT": "implement", // "Implement" triggers implement phase
				"GOAL_HINT":  "Implement Redis caching with write-through consistency",
			},
		},
		{
			name:   "complex prompt - multiple hints",
			prompt: "Goal: Build OAuth2 system\n\nBased on CID abc-123, **design** the token generation flow.\nWhat security considerations? How do we handle refresh tokens?",
			expected: map[string]string{
				"PHASE_HINT": "design",
				"TEMP_HINT":  "medium",
				"REF_CID":    "abc-123",
				"GOAL_HINT":  "Build OAuth2 system",
			},
		},
		{
			name:     "no hints - neutral prompt",
			prompt:   "Tell me about Redis caching.",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferFlowHints(tt.prompt)

			// Check expected keys are present with correct values
			for key, expectedVal := range tt.expected {
				if gotVal, ok := got[key]; !ok {
					t.Errorf("Expected key %q not found in result", key)
				} else if gotVal != expectedVal {
					t.Errorf("Key %q: expected %q, got %q", key, expectedVal, gotVal)
				}
			}

			// Check no unexpected keys are present
			for key := range got {
				if _, ok := tt.expected[key]; !ok {
					t.Errorf("Unexpected key %q found in result with value %q", key, got[key])
				}
			}
		})
	}
}

func TestHasKeywords(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
		expected bool
	}{
		{
			name:     "keyword present",
			text:     "design the system architecture",
			keywords: []string{"design", "architect"},
			expected: true,
		},
		{
			name:     "keyword absent",
			text:     "implement the feature",
			keywords: []string{"design", "architect"},
			expected: false,
		},
		{
			name:     "multiple keywords, one present",
			text:     "build the application",
			keywords: []string{"design", "build", "create"},
			expected: true,
		},
		{
			name:     "case insensitive",
			text:     "Design the System",
			keywords: []string{"design"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasKeywords(tt.text, tt.keywords)
			if got != tt.expected {
				t.Errorf("hasKeywords(%q, %v) = %v, expected %v", tt.text, tt.keywords, got, tt.expected)
			}
		})
	}
}

func TestExtractCIDReference(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "from CID pattern",
			text:     "Based on the exploration from CID abc-123-def, implement Redis",
			expected: "abc-123-def",
		},
		{
			name:     "bracket pattern",
			text:     "Refer to conversation [CID: xyz-789] for requirements",
			expected: "xyz-789",
		},
		{
			name:     "CID with UUID",
			text:     "from CID 550e8400-e29b-41d4-a716-446655440000",
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "no CID reference",
			text:     "Just a regular prompt without any references",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCIDReference(tt.text)
			if got != tt.expected {
				t.Errorf("extractCIDReference(%q) = %q, expected %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestExtractGoal(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "I want to pattern",
			text:     "I want to build a distributed tracing system",
			expected: "build a distributed tracing system",
		},
		{
			name:     "Let's pattern",
			text:     "Let's create an OAuth2 authentication provider",
			expected: "create an OAuth2 authentication provider",
		},
		{
			name:     "Goal: pattern",
			text:     "Goal: Implement Redis caching with write-through consistency",
			expected: "Implement Redis caching with write-through consistency",
		},
		{
			name:     "multiple lines with goal",
			text:     "Some context here.\n\nGoal: Build the system\n\nMore text.",
			expected: "Build the system",
		},
		{
			name:     "no goal present",
			text:     "Just a regular prompt",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoal(tt.text)
			if got != tt.expected {
				t.Errorf("extractGoal(%q) = %q, expected %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestBuildFlowHints(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected []string // substrings that should appear in output
	}{
		{
			name: "phase hint",
			env: map[string]string{
				"AIPHASE_HINT": "design",
			},
			expected: []string{
				"Context suggests this is a **design** phase",
			},
		},
		{
			name: "temperature hint",
			env: map[string]string{
				"AITEMP_HINT": "high",
			},
			expected: []string{
				"User emphasis suggests **high temperature** thinking",
			},
		},
		{
			name: "CID reference",
			env: map[string]string{
				"AIREF_CID": "abc-123",
			},
			expected: []string{
				"User references context from conversation **abc-123**",
			},
		},
		{
			name: "goal hint",
			env: map[string]string{
				"AIGOAL_HINT": "Build OAuth2 system",
			},
			expected: []string{
				"Working toward goal: **Build OAuth2 system**",
			},
		},
		{
			name: "markdown structure awareness always present",
			env:  map[string]string{},
			expected: []string{
				"Interpret markdown naturally",
			},
		},
		{
			name: "multiple hints",
			env: map[string]string{
				"AIPHASE_HINT": "design",
				"AITEMP_HINT":  "high",
				"AIGOAL_HINT":  "Build system",
			},
			expected: []string{
				"Context suggests this is a **design** phase",
				"User emphasis suggests **high temperature** thinking",
				"Working toward goal: **Build system**",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				ENV: tt.env,
			}
			got := buildFlowHints(ctx)

			for _, substr := range tt.expected {
				if !strings.Contains(got, substr) {
					t.Errorf("buildFlowHints() output missing expected substring: %q\nGot: %s", substr, got)
				}
			}
		})
	}
}

// TestTruncateEdgeCases tests edge cases in truncate function
func TestTruncateEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "negative maxLen returns empty",
			input:    "hello world",
			maxLen:   -1,
			expected: "",
		},
		{
			name:     "zero maxLen returns empty",
			input:    "hello world",
			maxLen:   0,
			expected: "",
		},
		{
			name:     "maxLen 1 returns ellipsis",
			input:    "hello",
			maxLen:   1,
			expected: "...",
		},
		{
			name:     "maxLen 3 returns ellipsis",
			input:    "hello world",
			maxLen:   3,
			expected: "...",
		},
		{
			name:     "maxLen 4 truncates with ellipsis",
			input:    "hello",
			maxLen:   4,
			expected: "h...",
		},
		{
			name:     "string shorter than maxLen returns unchanged",
			input:    "hi",
			maxLen:   10,
			expected: "hi",
		},
		{
			name:     "string equal to maxLen returns unchanged",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "empty string returns empty",
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, expected %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

// TestLoadReferencedContextEdgeCases tests edge cases in LoadReferencedContext
func TestLoadReferencedContextEdgeCases(t *testing.T) {
	t.Run("empty CID returns error", func(t *testing.T) {
		_, err := LoadReferencedContext("", 10)
		if err == nil {
			t.Error("LoadReferencedContext with empty CID should return error")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("Error should mention empty CID, got: %v", err)
		}
	})

	t.Run("negative maxMessages uses default", func(t *testing.T) {
		// This should not panic and should use default of 20
		_, err := LoadReferencedContext("nonexistent-cid", -1)
		// It will error because the CID doesn't exist, but shouldn't panic
		if err == nil {
			t.Error("Expected error for nonexistent CID")
		}
	})
}

// TestInferFlowHintsMalformedMarkdown tests handling of malformed markdown
func TestInferFlowHintsMalformedMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		checkKey string // key to check exists or doesn't cause panic
	}{
		{
			name:     "excessive asterisks",
			prompt:   "******** design this",
			checkKey: "PHASE_HINT",
		},
		{
			name:     "unmatched bold markers",
			prompt:   "**bold but not closed",
			checkKey: "TEMP_HINT",
		},
		{
			name:     "mixed emphasis",
			prompt:   "***triple*** **double** *single*",
			checkKey: "TEMP_HINT",
		},
		{
			name:     "empty prompt",
			prompt:   "",
			checkKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			got := InferFlowHints(tt.prompt)
			// Just verify it returns a map without panicking
			if got == nil {
				t.Error("InferFlowHints should never return nil")
			}
		})
	}
}
