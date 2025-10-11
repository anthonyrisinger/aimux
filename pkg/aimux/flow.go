package aimux

// flow.go - Session and subprocess management

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// MaxLineLength is the maximum length of a single log line (1MB)
	// to prevent out-of-memory errors from malformed output.
	MaxLineLength = 1024 * 1024

	// MaxOutputSize is the maximum total output size (10MB)
	// before truncation to prevent unbounded memory growth.
	MaxOutputSize = 10 * 1024 * 1024
)

// CommandStream wraps an exec.Cmd and its stdout pipe for proper cleanup.
type CommandStream struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	ctx     context.Context
	cancel  context.CancelFunc
	timeout time.Duration
}

func (cs *CommandStream) Read(p []byte) (n int, err error) {
	select {
	case <-cs.ctx.Done():
		return 0, cs.ctx.Err()
	default:
	}
	return cs.stdout.Read(p)
}

func (cs *CommandStream) Close() error {
	defer cs.cancel()

	if err := cs.stdout.Close(); err != nil {
		cs.killProcessGroup()
		_ = cs.cmd.Wait() // Reap process, ignore error (already have stdout error)
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cs.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		cs.killProcessGroup()
		// Wait for process to be reaped after SIGKILL
		_ = cs.cmd.Wait() // Reap process, ignore error (already have timeout error)
		return fmt.Errorf("command did not exit cleanly, killed after timeout")
	}
}

func (cs *CommandStream) killProcessGroup() {
	if cs.cmd == nil || cs.cmd.Process == nil {
		return
	}
	if runtime.GOOS != "windows" {
		if err := syscall.Kill(-cs.cmd.Process.Pid, syscall.SIGKILL); err != nil {
			Warn("Failed to kill process group: %v", err)
		}
	} else {
		if err := cs.cmd.Process.Kill(); err != nil {
			Warn("Failed to kill process: %v", err)
		}
	}
}

// LazyCommandStream delays subprocess start until first Read().
type LazyCommandStream struct {
	cmd     *exec.Cmd
	ctx     context.Context
	cancel  context.CancelFunc
	timeout time.Duration

	once   sync.Once
	stream *CommandStream
	err    error
}

func (lcs *LazyCommandStream) Read(p []byte) (n int, err error) {
	lcs.once.Do(func() {
		lcs.stream, lcs.err = lcs.start()
	})

	if lcs.err != nil {
		return 0, lcs.err
	}

	return lcs.stream.Read(p)
}

func (lcs *LazyCommandStream) Close() error {
	lcs.once.Do(func() {
		lcs.cancel()
	})

	if lcs.stream != nil {
		return lcs.stream.Close()
	}
	return nil
}

func (lcs *LazyCommandStream) start() (*CommandStream, error) {
	stdout, err := lcs.cmd.StdoutPipe()
	if err != nil {
		lcs.cancel()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := lcs.cmd.Start(); err != nil {
		lcs.cancel()
		return nil, fmt.Errorf("start %s: %w", lcs.cmd.Path, err)
	}

	return &CommandStream{
		cmd:     lcs.cmd,
		stdout:  stdout,
		ctx:     lcs.ctx,
		cancel:  lcs.cancel,
		timeout: lcs.timeout,
	}, nil
}

// NewID generates a new UUID v4 identifier.
func NewID() (ID, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return ID(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])), nil
}

// InitContext creates a new context with fresh CID and SID.
func InitContext(gen, model string) (*Context, error) {
	if err := ValidateContextParams("", gen, model); err != nil {
		return nil, err
	}

	cid, err := NewID()
	if err != nil {
		return nil, err
	}

	ctx := &Context{
		CID: cid,
		SID: cid,
		TOP: "",
		TAG: "",
		GEN: gen,
		MOD: model,
		LVL: 0,
		WTF: false,
		ENV: make(map[string]string),
	}

	if envTag := os.Getenv("AITAG"); envTag != "" {
		ctx.TOP = envTag
	}

	if envLvl := os.Getenv("AILVL"); envLvl != "" {
		if lvl, err := strconv.Atoi(envLvl); err == nil {
			ctx.LVL = lvl
		}
	}

	ctx.TAG = Tag3(ctx)

	dir, err := Dir2(ctx)
	if err != nil {
		return nil, err
	}
	ctx.DIR = dir

	// NOTE: We intentionally do NOT create directories or save context here.
	// That happens in StreamAndLog after first output line, to avoid creating
	// artifacts if the subprocess fails immediately.

	return ctx, nil
}

// DetermineSessionID determines the session ID for the current context.
// Checks (in order): context.json, log2, log1 (if undifferentiated), or returns CID.
func DetermineSessionID(ctx *Context) (ID, error) {
	contextPath := filepath.Join(ctx.DIR, contextFileName)
	if data, err := os.ReadFile(contextPath); err == nil {
		var savedCtx Context
		if err := json.Unmarshal(data, &savedCtx); err == nil && savedCtx.SID != "" {
			Debug("Found SID in context.json: %s", savedCtx.SID)
			return savedCtx.SID, nil
		}
	}

	log2, err := Log2(ctx)
	if err != nil {
		return "", err
	}
	log1, err := Log1(ctx)
	if err != nil {
		return "", err
	}

	if hasContent(log2) {
		sid, err := lastSessionID(log2)
		if err != nil {
			return "", err
		}
		ctx.SID = sid
		return sid, nil
	}

	if ctx.MOD == "" && hasContent(log1) {
		sid, err := lastSessionID(log1)
		if err != nil {
			return "", err
		}
		ctx.SID = sid
		return sid, nil
	}

	return ctx.CID, nil
}

// ResumeContext loads an existing context for the given CID.
func ResumeContext(cid ID, gen, model string) (*Context, error) {
	if err := ValidateContextParams(string(cid), gen, model); err != nil {
		return nil, err
	}

	ctx := &Context{
		CID: cid,
		SID: cid,
		TOP: "",
		TAG: "",
		GEN: gen,
		MOD: model,
		LVL: 0,
		WTF: false,
		ENV: make(map[string]string),
	}

	if envTag := os.Getenv("AITAG"); envTag != "" {
		ctx.TOP = envTag
	}

	if envLvl := os.Getenv("AILVL"); envLvl != "" {
		if lvl, err := strconv.Atoi(envLvl); err == nil {
			ctx.LVL = lvl
		}
	}

	ctx.TAG = Tag3(ctx)

	dir, err := Dir2(ctx)
	if err != nil {
		return nil, err
	}
	ctx.DIR = dir

	sid, err := DetermineSessionID(ctx)
	if err != nil {
		return nil, err
	}
	ctx.SID = sid

	// Check if conversation exists by verifying context.json file
	ctxPath := filepath.Join(ctx.DIR, contextFileName)
	if _, err := os.Stat(ctxPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("conversation does not exist")
	}

	if data, err := os.ReadFile(ctxPath); err == nil {
		var saved Context
		if err := json.Unmarshal(data, &saved); err == nil {
			// Preserve ENV map initialization, only copy if saved.ENV is non-nil
			if saved.ENV != nil {
				ctx.ENV = saved.ENV
			}
			ctx.WTF = saved.WTF
		}
	}

	return ctx, nil
}

// Branch creates a new session ID within the current conversation.
func Branch(ctx *Context) error {
	newSID, err := NewID()
	if err != nil {
		return err
	}
	ctx.SID = newSID
	ctx.LVL = 0

	dir, err := Dir2(ctx)
	if err != nil {
		return err
	}
	ctx.DIR = dir

	if err := os.MkdirAll(ctx.DIR, 0o755); err != nil {
		return fmt.Errorf("create branch directory %s: %w", ctx.DIR, err)
	}

	if err := saveContext(ctx); err != nil {
		return err
	}

	return nil
}

// CallGenus invokes the genus CLI with config-driven arg construction.
// stdin is passed as io.Reader to allow streaming - genus controls when/how to consume it.
//
// Returns io.ReadCloser with metadata about whether we passed explicit session-id.
// Metadata can be extracted via type assertion to *LazyCommandStream.
func CallGenus(ctx context.Context, c *Context, cmdArgs string, stdin io.Reader) (io.ReadCloser, error) {
	if err := ValidateCall(c); err != nil {
		return nil, err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	genus, ok := cfg.GetGenus(c.GEN)
	if !ok {
		return nil, fmt.Errorf("unknown genus: %s", c.GEN)
	}

	if len(genus.Exe) == 0 {
		return nil, fmt.Errorf("genus %s has no exe configured", c.GEN)
	}
	if err := ValidateCommand(genus.Exe[0]); err != nil {
		return nil, err
	}

	// Check for model override from HUD parsing
	// c.MOD provides behavioral persona, ENV["AIMODEL"] can override model selection
	modelPersona := c.MOD
	if override := c.ENV["AIMODEL"]; override != "" {
		modelPersona = override
		Debug("Model override active: using %s instead of %s for model selection", override, c.MOD)
	}

	personaVars := cfg.GetGenusPersonaVars(c.GEN, modelPersona)

	// For bash genus: if both cmdArgs and stdin provided, use bash -c to execute command
	// This allows: echo "input" | ./aimux -gen=bash "cat" to work properly
	var useBashC bool
	Debug("Prompt assembly: genus.Exe=%v cmdArgs=%q stdin=%v", genus.Exe, cmdArgs, stdin != nil)
	if genus.Exe[0] == "bash" && cmdArgs != "" && stdin != nil {
		useBashC = true
		Debug("Using bash -c mode: cmdArgs will be passed as -c argument, stdin will be piped")
	}

	args := []string{}

	// Start with genus.Cmd (always-prepended args)
	args = append(args, genus.Cmd...)

	// Add -c flag for bash genus when needed
	if useBashC {
		args = append(args, "-c", cmdArgs)
	}

	if len(genus.Args.Model) > 0 {
		args = append(args, RenderFlags(genus.Args.Model, personaVars)...)
	}

	sessionArgs, isNew, err := buildSessionFlags(c, genus, personaVars)
	if err != nil {
		return nil, err
	}
	args = append(args, sessionArgs...)

	// Store whether this is a NEW session (we control the session-id)
	c.ENV["_AIMUX_NEW_SESSION"] = fmt.Sprintf("%v", isNew)

	if len(genus.Args.Output) > 0 {
		args = append(args, genus.Args.Output...)
	}

	if len(genus.Args.Safety) > 0 {
		args = append(args, genus.Args.Safety...)
	}

	// Use custom system prompt if provided, otherwise generate protocol prompt
	var systemPrompt string
	if customPrompt := c.ENV["AISYS"]; customPrompt != "" {
		systemPrompt = customPrompt
	} else {
		// Generate system prompt with incremented depth for subprocess perspective
		// Shell increments AILVL before generating system prompt
		promptCtx := *c
		promptCtx.LVL++
		systemPrompt = Sys(&promptCtx)
	}

	// Handle system prompt injection and stdin
	var stdinContent io.Reader
	switch sp := genus.Args.Prompt.(type) {
	case string:
		if sp == "stdin" {
			// Genus takes system prompt via stdin - prepend to stdin stream
			if stdin != nil {
				stdinContent = io.MultiReader(strings.NewReader(systemPrompt+"\n\n"), stdin)
			} else if cmdArgs != "" {
				stdinContent = strings.NewReader(systemPrompt + "\n\n" + cmdArgs)
			} else {
				stdinContent = strings.NewReader(systemPrompt)
			}
		}
	case []interface{}:
		// Genus takes system prompt via args
		template := make([]string, len(sp))
		for i, v := range sp {
			template[i] = fmt.Sprint(v)
		}
		promptVars := map[string]string{"prompt": systemPrompt}
		args = append(args, RenderFlags(template, promptVars)...)
		// Handle cmdArgs and stdin
		// EXCEPT for bash -c mode: cmdArgs is already used as -c argument, don't prepend to stdin
		if useBashC {
			// bash -c mode: cmdArgs is the command, stdin is the input
			stdinContent = stdin
		} else if stdin != nil && cmdArgs != "" {
			// Normal mode: prepend cmdArgs to stdin
			stdinContent = io.MultiReader(strings.NewReader(cmdArgs+"\n\n"), stdin)
		} else if stdin != nil {
			stdinContent = stdin
		} else if cmdArgs != "" {
			stdinContent = strings.NewReader(cmdArgs)
		}
	default:
		// No special prompt handling - handle cmdArgs and stdin
		// EXCEPT for bash -c mode: cmdArgs is already used as -c argument, don't prepend to stdin
		if useBashC {
			// bash -c mode: cmdArgs is the command, stdin is the input
			stdinContent = stdin
		} else if stdin != nil && cmdArgs != "" {
			// Normal mode: prepend cmdArgs to stdin
			stdinContent = io.MultiReader(strings.NewReader(cmdArgs+"\n\n"), stdin)
		} else if stdin != nil {
			stdinContent = stdin
		} else if cmdArgs != "" {
			stdinContent = strings.NewReader(cmdArgs)
		}
	}

	// Create context with timeout (default 30 minutes, configurable)
	timeout := 30 * time.Minute
	if c.ENV["AITIMEOUT"] != "" {
		if t, err := time.ParseDuration(c.ENV["AITIMEOUT"]); err == nil && t > 0 {
			timeout = t
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)

	// Create the command with timeout context
	// Combine: genus.Exe (executable path) + remaining exe elements + args
	fullArgs := append(genus.Exe[1:], args...)
	Debug("Executing: %s %v", genus.Exe[0], fullArgs)
	cmd := exec.CommandContext(cmdCtx, genus.Exe[0], fullArgs...)

	// Set process group for proper cleanup (Unix only)
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	// Pass environment variables to subprocess
	cmd.Env = os.Environ()

	// Export aimux-specific variables for subprocess (matching shell script)
	// AITAG: who the subprocess is (current tag)
	// AITOP: who called this instance (for nested call detection)
	// AILVL: incremented depth (for depth limit enforcement in nested calls)
	// Shell: AITAG becomes AITOP in child, AILVL increments
	cmd.Env = append(cmd.Env, fmt.Sprintf("AICID=%s", c.CID))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AISID=%s", c.SID))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AIGEN=%s", c.GEN))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AIMOD=%s", c.MOD))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AITAG=%s", c.TAG))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AITOP=%s", c.TOP))
	cmd.Env = append(cmd.Env, fmt.Sprintf("AILVL=%d", c.LVL+1))

	if c.WTF {
		cmd.Env = append(cmd.Env, "AIWTF=1")
	}

	cmd.Stdin = stdinContent

	return &LazyCommandStream{
		cmd:     cmd,
		ctx:     cmdCtx,
		cancel:  cancel,
		timeout: timeout,
	}, nil
}

// buildSessionFlags constructs session management flags based on log file state.
// Returns the flags and a boolean indicating if we're starting a NEW session (true)
// vs resuming/branching (false). This helps StreamAndLog know whether to accept
// session_id from subprocess output.
func buildSessionFlags(c *Context, genus GenusConfig, personaVars PersonaVars) ([]string, bool, error) {
	log2, err := Log2(c)
	if err != nil {
		return nil, false, err
	}
	log1, err := Log1(c)
	if err != nil {
		return nil, false, err
	}

	sidVars := map[string]string{"sid": string(c.SID)}
	for k, v := range personaVars {
		sidVars[k] = v
	}

	// Check if log2 has established session (assistant responses present)
	if hasEstablishedSession(log2) {
		return RenderFlags(genus.Args.Resume, sidVars), false, nil
	}

	// Check if log1 has established session (assistant responses present)
	if hasEstablishedSession(log1) {
		// Check if log2 exists (even if empty) - indicates we're branching
		if fileExists(log2) && len(genus.Args.Branch) > 0 {
			return RenderFlags(genus.Args.Branch, sidVars), false, nil
		}
		return RenderFlags(genus.Args.Resume, sidVars), false, nil
	}

	// No existing session - need to start new one
	//
	// Only generate new SID for branching if:
	// 1. log2 exists (persona-specific log has history), AND
	// 2. CID != SID (we're already branched, not a fresh conversation)
	//
	// If CID == SID, this is a fresh conversation and we should preserve that.
	if fileExists(log2) && c.CID != c.SID {
		newSID, err := NewID()
		if err != nil {
			return nil, false, err
		}
		// Update SID in context for CLI arg, but don't save yet
		// StreamAndLog will save it only if the call succeeds
		c.SID = newSID
		sidVars["sid"] = string(newSID)
		Debug("Generated new SID for branching: %s (will save if call succeeds)", newSID)
	}

	// Fresh start - we're passing explicit --session-id, so don't accept session_id from output
	return RenderFlags(genus.Args.New, sidVars), true, nil
}

// detectFormat determines the output format based on first non-empty line
func detectFormat(firstLine string) string {
	if len(firstLine) == 0 {
		return "empty"
	}

	// Check first character for format detection
	switch firstLine[0] {
	case '{', '[':
		return "json"
	case '<':
		return "xml"
	default:
		return "text"
	}
}

// StreamAndLog reads output from genus command, detects format, and handles accordingly.
// Supports JSON (Claude CLI), plain text (cat/echo), and extensible for XML/other formats.
// (Equivalent to ai:claude:cat and ai::cat pipelines in shell version)
//
// IMPORTANT: Delays filesystem operations (creating directories, opening log files) until
// after the first line is read. This prevents creating artifacts if the command fails early.
func StreamAndLog(c *Context, r io.Reader, w io.Writer) error {
	// Use buffered writer for better performance
	bufWriter := bufio.NewWriter(w)
	defer bufWriter.Flush()

	// Set max line length to prevent OOM
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineLength)

	// Track total output size for limiting
	totalOutput := 0
	lineNumber := 0

	// Detect format from first non-empty line
	var format string

	// Track if we've seen an error in this stream (prevents SID updates)
	streamHasError := false

	// Track new SID from assistant messages (only apply if no errors)
	var pendingSID ID
	sidSaved := false  // Track if we've already saved the pending SID
	sidLogged := false // Track if we've already logged the pending SID

	// Delay log file creation until after first line
	var logFile *os.File
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Detect format on first non-empty line
		if format == "" && line != "" {
			format = detectFormat(line)
			Debug("Detected output format: %s (first char: %c)", format, line[0])

			// NOW create directories and context after we have first successful output
			if err := os.MkdirAll(c.DIR, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", c.DIR, err)
			}
			if err := saveContext(c); err != nil {
				Warn("Failed to save context: %v", err)
			}

			// Create log file
			log3, err := Log3(c)
			if err != nil {
				return err
			}
			logFile, err = os.OpenFile(log3, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("open log file: %w", err)
			}
		}

		// Check if we've exceeded output limit
		if totalOutput >= MaxOutputSize {
			Warn("Output size limit reached (%d bytes), truncating response", MaxOutputSize)
			if _, err := bufWriter.WriteString("\n[WARNING: Output truncated at 10MB limit]\n"); err != nil {
				Error("Failed to write truncation warning: %v", err)
			}
			break
		}

		// Handle line based on detected format
		switch format {
		case "json":
			// Parse JSON line
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(line), &data); err != nil {
				// Log warning for malformed JSON with context
				Warn("Malformed JSON at line %d (length %d): %v", lineNumber, len(line), err)
				if len(line) > 100 {
					Debug("Malformed JSON content (first 100 chars): %s...", line[:100])
				} else {
					Debug("Malformed JSON content: %s", line)
				}
				continue // Skip malformed JSON
			}

			// Check for error responses using is_error field (no heuristics)
			msgType, hasType := data["type"].(string)
			isError, _ := data["is_error"].(bool)

			// Mark stream as having error if we see is_error==true anywhere
			if isError {
				streamHasError = true
				// Clear pending SID immediately - don't save error sessions
				if pendingSID != "" {
					Debug("Discarding pending SID %s due to error in stream", pendingSID)
					pendingSID = ""
				}
			}

			// Handle explicit error types (type=="error" or type=="result" with is_error)
			if hasType && (msgType == "error" || (msgType == "result" && isError)) {
				// Extract error message
				var errorMsg string

				// Check for error object
				if err, ok := data["error"].(map[string]interface{}); ok {
					if msg, ok := err["message"].(string); ok {
						errorMsg = msg
					}
				}

				// Check for result field (in result type)
				if errorMsg == "" {
					if result, ok := data["result"].(string); ok {
						errorMsg = result
					}
				}

				// Check for top-level message field
				if errorMsg == "" {
					if msg, ok := data["message"].(string); ok {
						errorMsg = msg
					}
				}

				if errorMsg == "" {
					errorMsg = "Unknown API error"
				}

				// Log error but don't update SID
				Warn("API error response (type=%s, is_error=%v): %s", msgType, isError, errorMsg)

				// Only output error message if it's type=="error"
				// For type=="result", the assistant message already displayed it
				if msgType == "error" {
					if _, err := bufWriter.WriteString(errorMsg + "\n"); err != nil {
						Error("Failed to write error output: %v", err)
					}
					bufWriter.Flush()
				}

				// Don't write to log file for errors - we don't want to persist failed attempts
				continue
			}

			// Write JSON lines with session_id to log immediately
			// Check both .session_id (Claude) and .sessionId (Codex)
			// Note: Errors will be logged too, but SID won't be updated (see streamHasError check)
			_, hasSessionID := data["session_id"]
			_, hasSessionIdAlt := data["sessionId"]
			if hasSessionID || hasSessionIdAlt {
				if _, err := logFile.WriteString(line + "\n"); err != nil {
					Warn("Failed to write to log file: %v", err)
					// Continue processing even if logging fails
				}
			}

			// Extract and output message text with error recovery
			var extractedText string

			// Claude CLI format: type=="assistant" with .message.content[].text
			if msgType, ok := data["type"].(string); ok && msgType == "assistant" {
				// Collect session ID from assistant message (defer update until stream end)
				// Only assistant messages indicate an established session
				// Check both .session_id (Claude) and .sessionId (Codex)
				sessionIDRaw, hasSessionID := data["session_id"]
				if !hasSessionID {
					sessionIDRaw, hasSessionID = data["sessionId"]
				}
				if hasSessionID {
					if sessionIDStr, ok := sessionIDRaw.(string); ok && sessionIDStr != "" {
						newSID := ID(sessionIDStr)
						// Only log first time we see a session ID (signal change)
						if pendingSID == "" && !sidLogged {
							pendingSID = newSID
							sidLogged = true
							Debug("Session established: %s", pendingSID)
						} else if pendingSID == "" {
							pendingSID = newSID
						}
					}
				}
				if msg, ok := data["message"].(map[string]interface{}); ok {
					if content, ok := msg["content"].([]interface{}); ok {
						for _, item := range content {
							if itemMap, ok := item.(map[string]interface{}); ok {
								if text, ok := itemMap["text"].(string); ok {
									extractedText += text
								}
							}
						}
					}
				}
			} else if msg, ok := data["message"].(map[string]interface{}); ok {
				// .message.content[].text at top level
				if content, ok := msg["content"].([]interface{}); ok {
					for _, item := range content {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if text, ok := itemMap["text"].(string); ok {
								extractedText += text
							}
						}
					}
				}
			} else if msg, ok := data["msg"].(map[string]interface{}); ok {
				// .msg.message (used by some genera like Codex)
				if message, ok := msg["message"].(string); ok {
					extractedText = message
				}
			}

			// Save pending SID immediately whenever it changes (as early as possible)
			// SID can shift mid-stream, so we save on every change
			// This allows concurrent sessions to fork with the latest SID
			if pendingSID != "" && !streamHasError && pendingSID != c.SID {
				Debug("Flushing SID change: %s -> %s", c.SID, pendingSID)
				c.SID = pendingSID
				sidSaved = true
				if err := saveContext(c); err != nil {
					Warn("Failed to flush SID: %v", err)
				}
			}

			// Write extracted text with error handling
			if extractedText != "" {
				totalOutput += len(extractedText)
				if _, err := bufWriter.WriteString(extractedText); err != nil {
					Error("Failed to write output: %v", err)
					return fmt.Errorf("write output: %w", err)
				}
				// Flush on newlines for responsiveness
				if strings.Contains(extractedText, "\n") {
					bufWriter.Flush()
				}
			}

		case "text", "empty":
			// Plain text output - display and log as assistant message
			totalOutput += len(line)
			if _, err := bufWriter.WriteString(line + "\n"); err != nil {
				Error("Failed to write output: %v", err)
				return fmt.Errorf("write output: %w", err)
			}
			bufWriter.Flush()

			// Log text as assistant message with proper structure
			if line != "" {
				msg := Message{
					SessionID: c.SID,
					At:        time.Now(),
					From:      "assistant",
					Body:      line,
				}
				msgJSON, err := json.Marshal(msg)
				if err == nil {
					if _, err := logFile.WriteString(string(msgJSON) + "\n"); err != nil {
						Warn("Failed to write message to log: %v", err)
					}
				}
			}

		default:
			// Unknown format - treat as plain text
			Debug("Unknown format, treating as text: %s", format)
			totalOutput += len(line)
			if _, err := bufWriter.WriteString(line + "\n"); err != nil {
				Error("Failed to write output: %v", err)
				return fmt.Errorf("write output: %w", err)
			}
		}
	}

	// Final flush
	if err := bufWriter.Flush(); err != nil {
		Error("Failed to flush output buffer: %v", err)
		return fmt.Errorf("flush output: %w", err)
	}

	if err := scanner.Err(); err != nil {
		Error("Error reading stream: %v", err)
		return fmt.Errorf("read stream: %w", err)
	}

	// Save context at end if we haven't already (e.g., no session ID updates)
	// or if we need to ensure context.json exists for new sessions
	if !streamHasError && !sidSaved {
		// Update SID if we have a pending one that wasn't saved early
		if pendingSID != "" && pendingSID != c.SID {
			Debug("Applying pending SID at end: %s -> %s", c.SID, pendingSID)
			c.SID = pendingSID
		}
		// Save context to ensure it exists (for initial calls without SID updates)
		if err := saveContext(c); err != nil {
			Warn("Failed to save context: %v", err)
		}
	}

	return nil
}

// AppendMessage logs a message to the session log in JSONL format.
func AppendMessage(c *Context, from string, body string) error {
	log3, err := Log3(c)
	if err != nil {
		return err
	}

	msg := Message{
		SessionID: c.SID,
		At:        time.Now(),
		From:      from,
		Body:      body,
		Tags:      nil,
	}

	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Ensure log directory exists
	if err := os.MkdirAll(filepath.Dir(log3), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(log3, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}

	return nil
}

// Helper functions

// saveContext writes context metadata to DIR/context.json.
func saveContext(c *Context) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	path := filepath.Join(c.DIR, contextFileName)
	// Write as single line with newline at end (matching log.jsonl format)
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// hasContent returns true if the file exists and has non-zero size.
func hasContent(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// hasEstablishedSession returns true if the log file has assistant responses,
// indicating an established conversation (not just a user message).
func hasEstablishedSession(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		// Check for assistant message or type==assistant (different formats)
		if from, ok := msg["from"].(string); ok && from != "user" {
			return true
		}
		if msgType, ok := msg["type"].(string); ok && msgType == "assistant" {
			return true
		}
	}
	return false
}

// fileExists returns true if the path exists (even if empty).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lastSessionID reads the last line of a JSONL log file and extracts
// the session_id or sessionId field.
func lastSessionID(path string) (ID, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var lastLine string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	if lastLine == "" {
		return "", fmt.Errorf("log file is empty")
	}

	// Parse JSON and extract session_id
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(lastLine), &data); err != nil {
		return "", err
	}

	// Try session_id first, then sessionId
	var sid string
	if s, ok := data["session_id"].(string); ok {
		sid = s
	} else if s, ok := data["sessionId"].(string); ok {
		sid = s
	} else {
		return "", fmt.Errorf("no session ID found in last log line")
	}

	// Validate the extracted session ID
	if !isValidUUID(sid) {
		return "", fmt.Errorf("invalid session ID format in log file: %s", sid)
	}

	return ID(sid), nil
}

// stripCodeBlocks removes fenced code blocks (```...```) and indented code from text.
// This prevents detecting patterns inside code examples.
func stripCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	inFencedBlock := false

	for _, line := range lines {
		// Toggle fenced block state on ``` lines
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFencedBlock = !inFencedBlock
			continue
		}

		// Skip lines inside fenced blocks or indented code (4+ spaces or tab)
		if inFencedBlock || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// InferFlowHints analyzes user prompt for organic flow control patterns.
// Detects: phase keywords, emphasis (bold/italic), CID references, and goals.
// Skips code blocks to avoid false positives from code examples.
// Returns map of hints to inject into Context.ENV as AIPHASE_HINT, AITEMP_HINT, etc.
func InferFlowHints(prompt string) map[string]string {
	hints := make(map[string]string)

	// Strip code blocks to avoid detecting patterns in code examples
	cleanPrompt := stripCodeBlocks(prompt)

	// Phase detection based on keywords (order matters - most specific first)
	lowerPrompt := strings.ToLower(cleanPrompt)
	questionCount := strings.Count(cleanPrompt, "?")

	if questionCount >= 3 {
		hints["PHASE_HINT"] = "explore"
	} else if hasKeywords(lowerPrompt, []string{"review", "critique", "evaluate"}) {
		hints["PHASE_HINT"] = "review"
	} else if hasKeywords(lowerPrompt, []string{"test", "verify", "check", "validate"}) {
		hints["PHASE_HINT"] = "test"
	} else if hasKeywords(lowerPrompt, []string{"design", "architect", "structure", "plan"}) {
		hints["PHASE_HINT"] = "design"
	} else if hasKeywords(lowerPrompt, []string{"implement", "build", "code", "write", "create"}) {
		hints["PHASE_HINT"] = "implement"
	}

	// Temperature hints from emphasis (markdown bold/italic)
	boldCount := strings.Count(prompt, "**") / 2 // Each bold pair = **text**
	remainingAsterisks := strings.Count(prompt, "*") - boldCount*4
	if remainingAsterisks < 0 {
		remainingAsterisks = 0 // Guard against malformed markdown
	}
	italicCount := remainingAsterisks / 2 // Remaining singles

	if boldCount >= 2 {
		hints["TEMP_HINT"] = "high"
	} else if boldCount == 1 || italicCount >= 1 {
		hints["TEMP_HINT"] = "medium"
	}

	// Cross-conversation references (use cleanPrompt to avoid code examples)
	if cidRef := extractCIDReference(cleanPrompt); cidRef != "" {
		hints["REF_CID"] = cidRef
	}

	// Goal extraction (use cleanPrompt to avoid code examples)
	if goal := extractGoal(cleanPrompt); goal != "" {
		hints["GOAL_HINT"] = goal
	}

	return hints
}

// hasKeywords checks if text contains any of the given keywords (case-insensitive).
func hasKeywords(text string, keywords []string) bool {
	lowerText := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lowerText, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// extractCIDReference detects conversation ID references in natural language.
// Matches patterns like: "from CID abc-123", "CID: xyz-789", "[CID: uuid]"
func extractCIDReference(text string) string {
	// Patterns match both full UUIDs and shorthand CIDs (e.g., abc-123, xyz-789)
	// UUID format: 8-4-4-4-12 hex digits
	// Shorthand: any alphanumeric with hyphens
	patterns := []string{
		// Full UUID patterns
		`(?i)from\s+CID[:\s]+([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`,
		`(?i)CID[:\s]+([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`,
		`(?i)\[CID[:\s]+([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\]`,
		// Shorthand patterns (e.g., abc-123, xyz-789)
		`(?i)from\s+CID[:\s]+([-0-9a-z]+)`,
		`(?i)CID[:\s]+([-0-9a-z]+)`,
		`(?i)\[CID[:\s]+([-0-9a-z]+)\]`,
	}

	// Try each pattern
	for _, pattern := range patterns {
		if match := regexpFindString(pattern, text); match != "" {
			return match
		}
	}

	return ""
}

// extractGoal infers goal from natural language patterns.
// Looks for: "Goal:", "I want to", "Let's", etc.
func extractGoal(text string) string {
	patterns := []string{
		`(?i)goal:\s*(.+?)(?:\n|$)`,
		`(?i)(?:I|we)\s+want\s+to\s+(.+?)(?:\n|$)`,
		`(?i)(?:let'?s|lets)\s+(.+?)(?:\n|$)`,
		`(?i)objective:\s*(.+?)(?:\n|$)`,
	}

	for _, pattern := range patterns {
		if match := regexpFindString(pattern, text); match != "" {
			// Clean up the match
			match = strings.TrimSpace(match)
			// Truncate if too long
			if len(match) > 100 {
				match = match[:97] + "..."
			}
			return match
		}
	}

	return ""
}

// regexpFindString is a helper that compiles a regex and finds the first captured group.
// Returns empty string if no match found.
func regexpFindString(pattern, text string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// LoadReferencedContext loads recent messages from a referenced conversation.
// Returns up to maxMessages recent messages from the conversation's log.
// Tries multiple log paths: undifferentiated -> architect -> engineer.
func LoadReferencedContext(refCID ID, maxMessages int) ([]Message, error) {
	if refCID == "" {
		return nil, fmt.Errorf("refCID cannot be empty")
	}

	if maxMessages <= 0 {
		maxMessages = 20 // Default to 20 messages
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	// Try multiple log paths in order of preference
	conversationDir := filepath.Join(homeDir, ".aimux", "conversations", string(refCID), "claude")
	logPaths := []string{
		filepath.Join(conversationDir, "log.jsonl"),              // Undifferentiated
		filepath.Join(conversationDir, "architect", "log.jsonl"), // Architect persona
		filepath.Join(conversationDir, "engineer", "log.jsonl"),  // Engineer persona
	}

	var messages []Message
	var lastErr error

	for _, logPath := range logPaths {
		messages, err = loadMessagesFromLog(logPath)
		if err == nil {
			// Successfully loaded from this path
			break
		}
		lastErr = err
	}

	if messages == nil {
		return nil, fmt.Errorf("no logs found for CID %s: %w", refCID, lastErr)
	}

	// Return last N messages
	if len(messages) > maxMessages {
		return messages[len(messages)-maxMessages:], nil
	}
	return messages, nil
}

// loadMessagesFromLog reads and parses a JSONL log file.
// Returns all messages in chronological order.
func loadMessagesFromLog(logPath string) ([]Message, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	defer file.Close()

	var messages []Message
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Skip malformed lines
			continue
		}
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan log: %w", err)
	}

	return messages, nil
}

// truncate truncates a string to maxLen characters, adding "..." if truncated.
// Returns "..." for maxLen <= 3, and empty string for maxLen <= 0.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return "..."
	}
	return s[:maxLen-3] + "..."
}
