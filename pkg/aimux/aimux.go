// Package aimux implements the AIMUX partner protocol for multi-agent AI orchestration.
package aimux

// aimux.go - Core types and functions for AIMUX partner protocol

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// Directory structure constants
	aimuxDir         = ".aimux"
	conversationsDir = "conversations"
	logFileName      = "log.jsonl"
	contextFileName  = "context.json"

	// Placeholder for empty persona in path construction
	emptyModPlaceholder = "-"
)

// ID represents a conversation or session identifier (UUID v4 format).
type ID string

// Context holds state for a partner-protocol interaction.
type Context struct {
	CID ID                `json:"cid"`
	SID ID                `json:"sid"`
	TOP string            `json:"top"`
	TAG string            `json:"tag"`
	GEN string            `json:"gen"`
	MOD string            `json:"mod"`
	LVL int               `json:"lvl"`
	WTF bool              `json:"wtf"`
	DIR string            `json:"dir,omitempty"`
	ENV map[string]string `json:"env,omitempty"`
}

// Message represents one interaction in JSONL log format.
type Message struct {
	SessionID ID        `json:"session_id"`
	At        time.Time `json:"at"`
	From      string    `json:"from"`
	Body      string    `json:"body"`
	Tags      []string  `json:"tags,omitempty"`
}

// Tag2 returns MOD~GEN format, always with tilde (for config lookups).
func Tag2(c *Context) string {
	return c.MOD + "~" + c.GEN
}

// Tag3 returns canonical tag format, collapsing to GEN when MOD is empty.
func Tag3(c *Context) string {
	if c.MOD != "" {
		return c.MOD + "~" + c.GEN
	}
	return c.GEN
}

// SigTop returns human-readable signature for TOP tag, or "Main User" if empty.
func SigTop(c *Context) string {
	if c.TOP == "" {
		return "Main User"
	}
	return formatSig(c.TOP)
}

// SigTag returns human-readable signature for current TAG.
func SigTag(c *Context) string {
	tag := Tag3(c)
	return formatSig(tag)
}

// formatSig converts "architect~claude" to "Architect Claude".
// Returns "Main User" for empty/malformed tags.
func formatSig(tag string) string {
	if tag == "" || tag == "~" {
		return "Main User"
	}

	// Malformed tag ending with separator
	if strings.HasSuffix(tag, "~") {
		return "Main User"
	}

	if strings.Contains(tag, "~") {
		parts := strings.SplitN(tag, "~", 2)
		mod := parts[0]
		gen := parts[1]

		// Malformed tag with empty genus
		if gen == "" {
			return "Main User"
		}

		if mod == "" {
			return capitalize(gen)
		}
		return capitalize(mod) + " " + capitalize(gen)
	}

	return capitalize(tag)
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	// UTF-8 safe capitalization
	runes := []rune(s)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

// Dir1 returns ~/.aimux/conversations/$CID/$GEN
func Dir1(c *Context) (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, aimuxDir, conversationsDir, string(c.CID), c.GEN), nil
}

// Dir2 returns Dir1 or Dir1/$MOD if MOD is set.
func Dir2(c *Context) (string, error) {
	dir1, err := Dir1(c)
	if err != nil {
		return "", err
	}
	if c.MOD != "" {
		return filepath.Join(dir1, c.MOD), nil
	}
	return dir1, nil
}

// Log1 returns Dir1/log.jsonl
func Log1(c *Context) (string, error) {
	dir1, err := Dir1(c)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir1, logFileName), nil
}

// Log2 returns Dir1/-/log.jsonl or Dir1/$MOD/log.jsonl
func Log2(c *Context) (string, error) {
	dir1, err := Dir1(c)
	if err != nil {
		return "", err
	}
	mod := c.MOD
	if mod == "" {
		mod = emptyModPlaceholder
	}
	return filepath.Join(dir1, mod, logFileName), nil
}

// Log3 returns Dir2/log.jsonl
func Log3(c *Context) (string, error) {
	dir2, err := Dir2(c)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir2, logFileName), nil
}

// Env returns all relevant AI environment variables formatted for display
// (equivalent to ai::env in shell). Returns sorted list of KEY=VALUE strings.
func Env(c *Context) []string {
	// Normalize UUIDs to lowercase before exporting
	cid := NormalizeUUID(string(c.CID))
	sid := NormalizeUUID(string(c.SID))

	vars := []string{
		fmt.Sprintf("AICID=%s", cid),
		fmt.Sprintf("AISID=%s", sid),
		fmt.Sprintf("AITOP=%s", c.TOP),
		fmt.Sprintf("AITAG=%s", Tag3(c)),
		fmt.Sprintf("AIGEN=%s", c.GEN),
		fmt.Sprintf("AIMOD=%s", c.MOD),
		fmt.Sprintf("AILVL=%d", c.LVL),
	}
	if c.WTF {
		vars = append(vars, "AIWTF=x")
	}
	// Add any extra ENV vars
	for k, v := range c.ENV {
		if strings.HasPrefix(k, "AI") {
			vars = append(vars, fmt.Sprintf("%s=%s", k, v))
		}
	}
	sort.Strings(vars)
	return vars
}

// homeDir returns the user's home directory, or an error if it cannot
// be determined.
func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return home, nil
}

// Sys generates the complete system prompt for partner protocol
// (equivalent to ai::sys in shell). Combines START, GUIDE, HINTS, CONTEXT, FINAL.
func Sys(c *Context) string {
	var sb strings.Builder
	sb.WriteString(SysStart(c))
	sb.WriteString(SysGuide(c))
	sb.WriteString(SysHints(c))

	// Add referenced context if present
	if refCtx := SysReferencedContext(c); refCtx != "" {
		sb.WriteString(refCtx)
	}

	sb.WriteString(SysFinal(c))
	return sb.String()
}

// SysStart generates the partner protocol header
// (equivalent to ai::sys::start in shell).
func SysStart(c *Context) string {
	var sb strings.Builder
	sb.WriteString("PARTNER PROTOCOL START:\n")
	sb.WriteString(fmt.Sprintf("- Remote caller is *%s* (me) seeking response on STDIO;\n", SigTop(c)))
	sb.WriteString(fmt.Sprintf("- Local callee is *%s* (you) connected to STDIO;\n", SigTag(c)))
	sb.WriteString("- Leave **now** if caller and callee match to avoid calling yourself!\n")

	// Add all AI env vars
	for _, env := range Env(c) {
		// Skip empty values (like the shell's /=$/d in sed)
		if !strings.HasSuffix(env, "=") {
			sb.WriteString(fmt.Sprintf("- %s\n", strings.Replace(env, "=", " is ", 1)))
		}
	}
	return sb.String()
}

// SysGuide generates standard protocol rules
// (equivalent to ai::sys::guide in shell).
func SysGuide(c *Context) string {
	var sb strings.Builder
	sb.WriteString("PARTNER PROTOCOL GUIDE:\n")
	sb.WriteString(fmt.Sprintf("- Honor caller *%s* (me) yet challenge all assumptions;\n", SigTop(c)))
	sb.WriteString(fmt.Sprintf("- Embody persona *%s* (you) for entirety of this call;\n", SigTag(c)))
	sb.WriteString("- Never use partner protocol to close *inbound* calls like this call;\n")
	sb.WriteString("- Always use partner protocol to open *outbound* calls via Bash Tool;\n")
	sb.WriteString("- 30-min timeouts are required to avoid *aborting* calls prematurely;\n")
	sb.WriteString("- Trust yourself and your own good judgment to respond appropriately!\n")
	return sb.String()
}

// buildFlowHints generates organic flow control hints from Context.ENV.
func buildFlowHints(c *Context) string {
	var sb strings.Builder

	// Phase detection
	if phase := c.ENV["AIPHASE_HINT"]; phase != "" {
		sb.WriteString(fmt.Sprintf("- Context suggests this is a **%s** phase;\n", phase))
	}
	// Temperature hints
	if tempHint := c.ENV["AITEMP_HINT"]; tempHint != "" {
		sb.WriteString(fmt.Sprintf("- User emphasis suggests **%s temperature** thinking;\n", tempHint))
	}
	// Cross-conversation references
	if refCID := c.ENV["AIREF_CID"]; refCID != "" {
		sb.WriteString(fmt.Sprintf("- User references context from conversation **%s**;\n", refCID))
	}
	// Goal tracking
	if goal := c.ENV["AIGOAL_HINT"]; goal != "" {
		sb.WriteString(fmt.Sprintf("- Working toward goal: **%s**;\n", goal))
	}
	// Temporal queries (rwd)
	if rwdTime := c.ENV["AIRWD"]; rwdTime != "" {
		sb.WriteString(fmt.Sprintf("- **TEMPORAL QUERY**: You are viewing conversation state as of **%s**;\n", rwdTime))
		sb.WriteString("- Respond from that historical perspective without knowledge of future events;\n")
	}

	// Markdown structure awareness (always include)
	sb.WriteString("- Interpret markdown naturally: **Headers** = phases, **Lists** = decisions, **Bold/Italic** = emphasis, **Code blocks** = artifacts;\n")

	return sb.String()
}

// SysHints generates dynamic persona-specific instructions.
// Checks templates first, then config, then falls back to built-in logic.
func SysHints(c *Context) string {
	var sb strings.Builder
	sb.WriteString("PARTNER PROTOCOL HINTS:\n")
	sb.WriteString("- Realize `... Claude,` (or Codex) is a shell alias and should be used VERBATIM;\n")

	// Check if persona has custom template hints in ~/.aimux/templates/hints/<persona>.txt
	if c.MOD != "" {
		if templateHints := LoadTemplateHints(c.MOD); len(templateHints) > 0 {
			for _, hint := range templateHints {
				sb.WriteString("- " + hint + "\n")
			}
			// Add organic flow hints
			sb.WriteString(buildFlowHints(c))
			sb.WriteString("- Run `ai::sys` in **Bash Tool** whenever needed to regenerate this system prompt!\n")
			return sb.String()
		}
	}

	// Load config (if it fails, skip config hints and use built-in fallback)
	cfg, err := LoadConfig()
	if err != nil {
		Debug("LoadConfig failed in SysHints, using built-in fallback: %v", err)
	} else if c.MOD != "" {
		// Check if persona has custom hints in config
		hints := cfg.GetPersonaHints(c.MOD)
		if len(hints) > 0 {
			for _, hint := range hints {
				sb.WriteString("- " + hint + "\n")
			}
			// Add organic flow hints
			sb.WriteString(buildFlowHints(c))
			sb.WriteString("- Run `ai::sys` in **Bash Tool** whenever needed to regenerate this system prompt!\n")
			return sb.String()
		}
	}

	// Built-in fallback for undifferentiated
	tag2 := Tag2(c)
	if tag2 == "~claude" {
		sb.WriteString("- **On FIRST read via Bash Tool**, run `Customer Claude,\\n\\nI am [NAME ...] the *new* [ROLE ...] I need rapid onboarding of your perspective!\\n`;\n")
		sb.WriteString("- **On FIRST read via Bash Tool**, run `Architect Claude,\\n\\nI am [NAME ...] the *new* [ROLE ...] I need rapid onboarding of your perspective!\\n`;\n")
		sb.WriteString("- CRITICAL: DELEGATE experiential and confirmational tasks to *Customer Claude* to enhance *your own* contextual awareness;\n")
		sb.WriteString("- CRITICAL: DELEGATE evaluational and implementation tasks to *Architect Claude* to protect *your own* contextual integrity;\n")
	}

	// Add organic flow hints
	sb.WriteString(buildFlowHints(c))

	sb.WriteString("- Run `ai::sys` in **Bash Tool** whenever needed to regenerate this system prompt!\n")
	return sb.String()
}

// SysFinal generates closing reminders
// (equivalent to ai::sys::final in shell).
func SysFinal(c *Context) string {
	var sb strings.Builder
	sb.WriteString("PARTNER PROTOCOL FINAL:\n")
	sb.WriteString("- **30-minute** timeouts on Bash Tool for ALL outbound partner calls;\n")
	sb.WriteString("- Accumulate and respect ALL stakeholder intent per your persona;\n")
	sb.WriteString("- CRITICAL: TRUST YOUR TEAM and STAY IN YOUR LANE!\n")
	return sb.String()
}

// SysReferencedContext loads and formats context from a referenced conversation.
// Returns formatted context block if AIREF_CID is set, otherwise empty string.
func SysReferencedContext(c *Context) string {
	refCID := c.ENV["AIREF_CID"]
	if refCID == "" {
		return ""
	}

	// Load recent messages from referenced conversation
	messages, err := LoadReferencedContext(ID(refCID), 10) // Limit to 10 messages
	if err != nil {
		// Silently fail if conversation not found
		return ""
	}

	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("PARTNER PROTOCOL CONTEXT:\n")
	sb.WriteString(fmt.Sprintf("- Referenced conversation: **%s**\n", refCID))
	sb.WriteString(fmt.Sprintf("- Showing last %d messages:\n", len(messages)))

	for i, msg := range messages {
		// Truncate long message bodies
		body := truncate(msg.Body, 200)
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, msg.From, body))
	}

	sb.WriteString("\n")
	return sb.String()
}

// SysError generates an error message for partner protocol violations.
// The errType parameter allows customization (e.g., "BLOCK", "ERROR").
func SysError(c *Context, errType string, message string) string {
	if errType == "" {
		errType = "ERROR"
	}
	msg := fmt.Sprintf("PARTNER PROTOCOL %s:\n", errType)
	if message != "" {
		msg += fmt.Sprintf("- Sorry, %s.\n", message)
	} else {
		msg += "- Sorry.\n"
	}
	return msg
}

// SysBlock is a convenience wrapper for SysError with type "BLOCK".
func SysBlock(c *Context, message string) string {
	return SysError(c, "BLOCK", message)
}

// BlockingError represents a partner protocol violation that should
// prevent a call from proceeding. It includes the exit code from the
// shell script's blocking logic.
type BlockingError struct {
	Code    int
	Message string
}

func (e *BlockingError) Error() string {
	return e.Message
}

// ValidateCall enforces partner protocol rules to prevent infinite recursion
// and maintain persona boundaries. It implements four blocking checks:
//
// 1. Depth check (code 3): Blocks if recursion depth >= 3 levels
// 2. Self-call check (code 1): Blocks if trying to call the exact same instance
// 3. Engineer restriction (code 4): Blocks engineers from making any calls
// 4. Undifferentiated→engineer check (code 5): Blocks this specific transition
//
// Returns nil if the call is allowed, or a BlockingError with the appropriate
// code and message if blocked.
func ValidateCall(c *Context) error {
	// Check 1: Depth exceeded (AILVL >= 3)
	if c.LVL >= 3 {
		return &BlockingError{
			Code:    3,
			Message: fmt.Sprintf("recursive call depth exceeded (%d)", c.LVL),
		}
	}

	// Check 2: Self-call prevention (TAG == TOP)
	// Prevents an agent from calling itself, which would create infinite recursion.
	// This also blocks coordinator calls from differentiated personas when they
	// attempt to spawn an undifferentiated instance.
	if c.TAG != "" && c.TAG == c.TOP {
		return &BlockingError{
			Code:    1,
			Message: fmt.Sprintf("you (%s) cannot call yourself", SigTag(c)),
		}
	}

	// Check 3: Engineer restriction
	// Engineers are leaf nodes in the call graph and cannot delegate further.
	// Only enforced during partner protocol calls (TOP is set).
	if c.TOP != "" && strings.Contains(c.TOP, "~") {
		topParts := strings.Split(c.TOP, "~")
		if topParts[0] == "engineer" {
			return &BlockingError{
				Code:    4,
				Message: fmt.Sprintf("you (%s) cannot call anyone; ask your caller instead", SigTop(c)),
			}
		}
	}

	// Check 4: Undifferentiated→engineer restriction
	// Prevents undifferentiated coordinators from calling engineers directly.
	// They must go through an architect to maintain proper delegation hierarchy.
	if c.TOP != "" && !strings.Contains(c.TOP, "~") && c.MOD == "engineer" {
		return &BlockingError{
			Code:    5,
			Message: fmt.Sprintf("you (%s) cannot call %s; ask your team instead", SigTop(c), SigTag(c)),
		}
	}

	return nil
}
