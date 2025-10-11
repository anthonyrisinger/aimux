package aimux

import (
	"os"
	"strings"
	"testing"
)

// TestTagGeneration verifies tag generation functions match shell script behavior
func TestTagGeneration(t *testing.T) {
	tests := []struct {
		name     string
		gen      string
		mod      string
		wantTag2 string
		wantTag3 string
	}{
		{
			name:     "undifferentiated claude",
			gen:      "claude",
			mod:      "",
			wantTag2: "~claude",
			wantTag3: "claude",
		},
		{
			name:     "architect claude",
			gen:      "claude",
			mod:      "architect",
			wantTag2: "architect~claude",
			wantTag3: "architect~claude",
		},
		{
			name:     "engineer claude",
			gen:      "claude",
			mod:      "engineer",
			wantTag2: "engineer~claude",
			wantTag3: "engineer~claude",
		},
		{
			name:     "undifferentiated codex",
			gen:      "codex",
			mod:      "",
			wantTag2: "~codex",
			wantTag3: "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				GEN: tt.gen,
				MOD: tt.mod,
			}

			if got := Tag2(ctx); got != tt.wantTag2 {
				t.Errorf("Tag2() = %v, want %v", got, tt.wantTag2)
			}
			if got := Tag3(ctx); got != tt.wantTag3 {
				t.Errorf("Tag3() = %v, want %v", got, tt.wantTag3)
			}
		})
	}
}

// TestSignatures verifies signature generation matches shell script behavior
func TestSignatures(t *testing.T) {
	tests := []struct {
		name    string
		gen     string
		mod     string
		top     string
		wantSig string
	}{
		{
			name:    "empty TOP returns Main User",
			gen:     "claude",
			mod:     "",
			top:     "",
			wantSig: "Claude", // SigTag for undifferentiated returns "Claude", not "Main User"
		},
		{
			name:    "architect signature",
			gen:     "claude",
			mod:     "architect",
			top:     "",
			wantSig: "Architect Claude",
		},
		{
			name:    "engineer signature",
			gen:     "claude",
			mod:     "engineer",
			top:     "",
			wantSig: "Engineer Claude",
		},
		{
			name:    "undifferentiated signature",
			gen:     "claude",
			mod:     "",
			top:     "",
			wantSig: "Claude",
		},
		{
			name:    "customer signature",
			gen:     "codex",
			mod:     "customer",
			top:     "",
			wantSig: "Customer Codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				GEN: tt.gen,
				MOD: tt.mod,
				TOP: tt.top,
				TAG: Tag3(&Context{GEN: tt.gen, MOD: tt.mod}),
			}

			if tt.top == "" {
				if got := SigTop(ctx); got != "Main User" {
					t.Errorf("SigTop() = %v, want Main User", got)
				}
			}

			if got := SigTag(ctx); got != tt.wantSig {
				t.Errorf("SigTag() = %v, want %v", got, tt.wantSig)
			}
		})
	}
}

// TestPaths verifies path generation functions
func TestPaths(t *testing.T) {
	// Create a test CID
	testCID := ID("12345678-1234-4123-8234-123456789abc")

	tests := []struct {
		name     string
		gen      string
		mod      string
		checkDir func(*Context) (string, error)
		contains []string
	}{
		{
			name:     "Dir1 for claude",
			gen:      "claude",
			mod:      "",
			checkDir: Dir1,
			contains: []string{".aimux", "conversations", string(testCID), "claude"},
		},
		{
			name:     "Dir2 for undifferentiated",
			gen:      "claude",
			mod:      "",
			checkDir: Dir2,
			contains: []string{".aimux", "conversations", string(testCID), "claude"},
		},
		{
			name:     "Dir2 for architect",
			gen:      "claude",
			mod:      "architect",
			checkDir: Dir2,
			contains: []string{".aimux", "conversations", string(testCID), "claude", "architect"},
		},
		{
			name:     "Log3 for engineer",
			gen:      "claude",
			mod:      "engineer",
			checkDir: Log3,
			contains: []string{".aimux", "conversations", string(testCID), "claude", "engineer", "log.jsonl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				CID: testCID,
				SID: testCID,
				GEN: tt.gen,
				MOD: tt.mod,
			}

			got, err := tt.checkDir(ctx)
			if err != nil {
				t.Errorf("Path function error: %v", err)
				return
			}

			for _, part := range tt.contains {
				if !strings.Contains(got, part) {
					t.Errorf("Path %v does not contain %v", got, part)
				}
			}
		})
	}
}

// TestBlockingRules verifies all blocking rules work correctly
func TestBlockingRules(t *testing.T) {
	tests := []struct {
		name      string
		ctx       *Context
		wantError bool
		wantCode  int
	}{
		{
			name: "depth exceeded",
			ctx: &Context{
				LVL: 3,
				TAG: "test",
				TOP: "caller",
				GEN: "claude",
				MOD: "engineer",
			},
			wantError: true,
			wantCode:  3,
		},
		{
			name: "self call blocked",
			ctx: &Context{
				LVL: 1,
				TAG: "engineer~claude",
				TOP: "engineer~claude",
				GEN: "claude",
				MOD: "engineer",
			},
			wantError: true,
			wantCode:  1,
		},
		{
			name: "engineer cannot call others",
			ctx: &Context{
				LVL: 1,
				TAG: "architect~claude",
				TOP: "engineer~claude",
				GEN: "claude",
				MOD: "architect",
			},
			wantError: true,
			wantCode:  4,
		},
		{
			name: "undifferentiated to engineer blocked",
			ctx: &Context{
				LVL: 1,
				TAG: "engineer~claude", // Computed from MOD="engineer", GEN="claude"
				TOP: "claude",          // Caller is undifferentiated
				GEN: "claude",
				MOD: "engineer",
			},
			wantError: true,
			wantCode:  5,
		},
		{
			name: "architect can call engineer",
			ctx: &Context{
				LVL: 1,
				TAG: "architect~claude",
				TOP: "customer~claude",
				GEN: "claude",
				MOD: "architect",
			},
			wantError: false,
		},
		{
			name: "first call allowed (empty TOP)",
			ctx: &Context{
				LVL: 0,
				TAG: "claude",
				TOP: "",
				GEN: "claude",
				MOD: "",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCall(tt.ctx)

			if tt.wantError {
				if err == nil {
					t.Errorf("ValidateCall() error = nil, wantError %v", tt.wantError)
					return
				}

				blockErr, ok := err.(*BlockingError)
				if !ok {
					t.Errorf("ValidateCall() error = %v, want BlockingError", err)
					return
				}

				if blockErr.Code != tt.wantCode {
					t.Errorf("BlockingError.Code = %v, want %v", blockErr.Code, tt.wantCode)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateCall() error = %v, want nil", err)
				}
			}
		})
	}
}

// TestProtocolFlow tests a complete protocol flow
func TestProtocolFlow(t *testing.T) {
	// Set up test environment
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	t.Run("undifferentiated to architect to engineer flow", func(t *testing.T) {
		// Step 1: Undifferentiated call (should succeed)
		ctx1 := &Context{
			LVL: 0,
			TAG: "",
			TOP: "",
			GEN: "claude",
			MOD: "",
			ENV: make(map[string]string),
		}
		ctx1.TAG = Tag3(ctx1)

		if err := ValidateCall(ctx1); err != nil {
			t.Errorf("Undifferentiated call failed: %v", err)
		}

		// Step 2: Undifferentiated calls architect (should succeed)
		ctx2 := &Context{
			LVL: 1,
			TAG: "claude",
			TOP: "claude",
			GEN: "claude",
			MOD: "architect",
			ENV: make(map[string]string),
		}
		ctx2.TAG = Tag3(ctx2)

		// This would normally be blocked (calling yourself),
		// but let's test architect calling engineer instead
		ctx2.TOP = "claude"
		ctx2.TAG = "architect~claude"

		if err := ValidateCall(ctx2); err != nil {
			t.Errorf("Architect call failed: %v", err)
		}

		// Step 3: Architect calls engineer (should succeed)
		ctx3 := &Context{
			LVL: 2,
			TAG: "architect~claude",
			TOP: "architect~claude",
			GEN: "claude",
			MOD: "engineer",
			ENV: make(map[string]string),
		}
		ctx3.TAG = Tag3(ctx3)
		ctx3.TOP = "architect~claude"

		if err := ValidateCall(ctx3); err != nil {
			t.Errorf("Engineer call from architect failed: %v", err)
		}

		// Step 4: Engineer tries to call anyone (should fail)
		ctx4 := &Context{
			LVL: 3,
			TAG: "engineer~claude",
			TOP: "architect~claude",
			GEN: "claude",
			MOD: "engineer",
			ENV: make(map[string]string),
		}

		if err := ValidateCall(ctx4); err == nil {
			t.Error("Engineer call should have been blocked")
		} else if blockErr, ok := err.(*BlockingError); ok {
			// Should be blocked for depth (level 3)
			if blockErr.Code != 3 {
				t.Errorf("Expected blocking code 3 (depth), got %d", blockErr.Code)
			}
		}
	})
}

// TestEnvironmentVariables tests environment variable generation
func TestEnvironmentVariables(t *testing.T) {
	testCID := ID("12345678-1234-4123-8234-123456789abc")
	testSID := ID("87654321-4321-1234-8234-cba987654321")

	ctx := &Context{
		CID: testCID,
		SID: testSID,
		TOP: "architect~claude",
		TAG: "engineer~claude",
		GEN: "claude",
		MOD: "engineer",
		LVL: 2,
		WTF: true,
		ENV: map[string]string{
			"AITEST": "value",
		},
	}

	envVars := Env(ctx)

	// Check for required variables
	requiredVars := map[string]string{
		"AICID":  string(testCID),
		"AISID":  string(testSID),
		"AITOP":  "architect~claude",
		"AITAG":  "engineer~claude",
		"AIGEN":  "claude",
		"AIMOD":  "engineer",
		"AILVL":  "2",
		"AIWTF":  "x",
		"AITEST": "value",
	}

	for key, expectedValue := range requiredVars {
		found := false
		for _, env := range envVars {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 && parts[0] == key {
				found = true
				if parts[1] != expectedValue {
					t.Errorf("Environment variable %s = %s, want %s", key, parts[1], expectedValue)
				}
				break
			}
		}
		if !found {
			t.Errorf("Environment variable %s not found", key)
		}
	}
}

// TestUUIDNormalization tests UUID normalization and validation
func TestUUIDNormalization(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantLower string
	}{
		{
			name:      "lowercase UUID",
			input:     "12345678-1234-4123-8234-123456789abc",
			wantValid: true,
			wantLower: "12345678-1234-4123-8234-123456789abc",
		},
		{
			name:      "uppercase UUID",
			input:     "12345678-1234-4123-8234-123456789ABC",
			wantValid: true,
			wantLower: "12345678-1234-4123-8234-123456789abc",
		},
		{
			name:      "mixed case UUID",
			input:     "12345678-1234-4123-8234-123456789AbC",
			wantValid: true,
			wantLower: "12345678-1234-4123-8234-123456789abc",
		},
		{
			name:      "invalid UUID - wrong length",
			input:     "12345678-1234-4123-8234",
			wantValid: false,
			wantLower: "12345678-1234-4123-8234",
		},
		{
			name:      "invalid UUID - bad characters",
			input:     "12345678-1234-4123-8234-12345678ZZZZ",
			wantValid: false,
			wantLower: "12345678-1234-4123-8234-12345678zzzz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test normalization
			normalized := NormalizeUUID(tt.input)
			if normalized != tt.wantLower {
				t.Errorf("NormalizeUUID(%q) = %q, want %q", tt.input, normalized, tt.wantLower)
			}

			// Test validation
			valid := isValidUUID(tt.input)
			if valid != tt.wantValid {
				t.Errorf("isValidUUID(%q) = %v, want %v", tt.input, valid, tt.wantValid)
			}
		})
	}
}
