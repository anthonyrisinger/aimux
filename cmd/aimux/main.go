package main

// main.go - CLI entry point for AIMUX partner protocol

import (
	"aimux/pkg/aimux"
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// parseHUDLine extracts persona, genus, and optional model override from first line.
// Splits on first non-alphanumeric delimiter (comma, colon, etc) to extract address tokens.
// Performs order-agnostic token classification to organically extract intent.
// Returns (persona, genus, modelOverride) where modelOverride is empty if not specified.
func parseHUDLine(cfg *aimux.Config, line string) (persona, genus, modelOverride string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", ""
	}

	// Find first non-alphanumeric/non-whitespace delimiter (comma, colon, etc)
	// This allows flexible addressing: "Architect Claude," or "Haiku Bash:"
	delimIdx := -1
	for i, r := range line {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == ' ' || r == '\t') {
			delimIdx = i
			break
		}
	}

	// Extract address part (everything before delimiter, or whole line if no delimiter)
	addressPart := line
	if delimIdx >= 0 {
		addressPart = line[:delimIdx]
	}

	// Split address part on whitespace and process all tokens
	tokens := strings.Fields(addressPart)
	if len(tokens) == 0 {
		return "", "", ""
	}

	// Classify all tokens order-agnostically
	for _, token := range tokens {
		normalized := strings.ToLower(stripPunctuation(token))
		typ := classifyToken(cfg, normalized)

		switch typ {
		case "genus":
			if genus == "" {
				genus = normalized
			}
		case "global_persona":
			if persona == "" {
				persona = normalized
			}
		case "model_name":
			if modelOverride == "" {
				modelOverride = normalized
			}
			// Infer genus from model if not already set
			if genus == "" {
				genus = inferGenus(cfg, normalized)
			}
		}
	}

	// Defaults and fallbacks
	if genus == "" {
		return "", "", ""
	}
	if persona == "" && modelOverride != "" {
		// "Haiku Claude," means use haiku as both model and persona
		persona = modelOverride
	} else if persona == "" {
		// Fallback to first token as persona
		persona = strings.ToLower(stripPunctuation(tokens[0]))
	}

	return persona, genus, modelOverride
}

// classifyToken determines what kind of token this is.
// Returns: "genus", "global_persona", "model_name", or "unknown"
func classifyToken(cfg *aimux.Config, token string) string {
	// Check if it's a known genus
	if _, ok := cfg.Genera[token]; ok {
		return "genus"
	}

	// Check if it's a global persona
	if _, ok := cfg.Personas[token]; ok {
		return "global_persona"
	}

	// Check if it's a model name in any genus
	for _, genus := range cfg.Genera {
		if _, ok := genus.Personas[token]; ok {
			return "model_name"
		}
	}

	return "unknown"
}

// inferGenus finds which genus contains this model/persona name.
// Returns empty string if not found.
func inferGenus(cfg *aimux.Config, modelName string) string {
	for genusName, genus := range cfg.Genera {
		if _, ok := genus.Personas[modelName]; ok {
			return genusName
		}
	}
	return "" // Not found
}

// stripPunctuation keeps only ASCII letters, digits, and hyphens.
// This is used to normalize user input tokens for matching against ASCII config keys.
func stripPunctuation(s string) string {
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// validGeneraList returns sorted list of valid genus names from config
func validGeneraList(cfg *aimux.Config) []string {
	genera := make([]string, 0, len(cfg.Genera))
	for name := range cfg.Genera {
		genera = append(genera, name)
	}
	return genera
}

// handleError checks if error is a BlockingError and exits with appropriate code/message
func handleError(ctx *aimux.Context, err error, prefix string) {
	if blockErr, ok := err.(*aimux.BlockingError); ok {
		fmt.Fprintf(os.Stderr, "%s\n", aimux.SysBlock(ctx, blockErr.Message))
		os.Exit(blockErr.Code)
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", prefix, err)
	os.Exit(1)
}

func main() {
	// Custom usage function with controlled flag order
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: aimux [options] <prompt>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  -new             start new session (or branch current)")
		fmt.Fprintln(os.Stderr, "  -gen=GENUS       generator/genus/type (claude, bash, codex)")
		fmt.Fprintln(os.Stderr, "  -mod=PERSONA     model/persona/role (architect, engineer, customer)")
		fmt.Fprintln(os.Stderr, "  -cid=UUID        conversation ID to start (or resume)")
		fmt.Fprintln(os.Stderr, "  -sid=UUID        session ID (overrides auto-detection)")
		fmt.Fprintln(os.Stderr, "  -lvl=N           call depth (overrides auto-detection)")
		fmt.Fprintln(os.Stderr, "  -top=TAG         caller tag (overrides auto-detection)")
		fmt.Fprintln(os.Stderr, "  -tag=TAG         callee tag (overrides auto-detection)")
		fmt.Fprintln(os.Stderr, "  -rwd=TIME        rewind to timestamp (RFC3339 format)")
		fmt.Fprintln(os.Stderr, "  -sys=PROMPT      custom system prompt (overrides generation)")
		fmt.Fprintln(os.Stderr, "  -hud             parse first stdin line for 'Persona Genus,' syntax")
		fmt.Fprintln(os.Stderr, "  -wtf             enable debug mode")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Organic Flow Control (automatic detection from prompt):")
		fmt.Fprintln(os.Stderr, "  - Phase hints: 'design', 'implement', 'review', etc.")
		fmt.Fprintln(os.Stderr, "  - Temperature: **bold** = high, *italic* = medium")
		fmt.Fprintln(os.Stderr, "  - CID references: 'from CID abc-123' loads context")
		fmt.Fprintln(os.Stderr, "  - Goals: 'Goal: build X' or 'I want to X'")
	}

	gen := flag.String("gen", "", "generator/genus/type (claude, codex, …)")
	mod := flag.String("mod", "", "model/persona/role (architect, engineer, opus, …)")
	cid := flag.String("cid", "", "conversation ID to resume")
	sid := flag.String("sid", "", "session ID to set (overrides auto-detection)")
	lvl := flag.Int("lvl", -1, "call depth level (overrides auto-detection)")
	top := flag.String("top", "", "caller tag (overrides environment)")
	tag := flag.String("tag", "", "callee tag (overrides computation)")
	rwd := flag.String("rwd", "", "rewind to timestamp (RFC3339 format)")
	sys := flag.String("sys", "", "custom system prompt (overrides generation)")
	hud := flag.Bool("hud", false, "parse first line for 'Persona Genus,' to set mod/gen")
	new := flag.Bool("new", false, "branch new session from existing conversation")
	wtf := flag.Bool("wtf", false, "enable debug mode")

	flag.Parse()

	// Enable debug mode early if requested (needed for config loading debug messages)
	if *wtf || os.Getenv("AIWTF") != "" {
		aimux.SetLevel(aimux.DEBUG)
	}

	// Load config early (needed for HUD token classification)
	cfg, cfgErr := aimux.LoadConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", cfgErr)
		os.Exit(1)
	}

	// Handle stdin EARLY - block on first line if piped to ensure proper ordering in pipelines
	// This prevents the process from proceeding until it's "our turn" in a pipe chain
	cmdArgs := strings.TrimSpace(strings.Join(flag.Args(), " "))
	var stdinReader io.Reader
	var modelOverride string
	stdinIsPipe := false

	if stdinStat, err := os.Stdin.Stat(); err == nil {
		// Check if stdin is NOT a TTY (i.e., it's a pipe or file)
		if (stdinStat.Mode() & os.ModeCharDevice) == 0 {
			stdinIsPipe = true
			aimux.Debug("Stdin is piped, reading first line to synchronize pipeline...")
			reader := bufio.NewReader(os.Stdin)
			firstLine, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
				os.Exit(1)
			}

			// If HUD mode, parse first line for "Persona Genus," syntax
			if *hud && firstLine != "" {
				parsedMod, parsedGen, parsedModel := parseHUDLine(cfg, firstLine)
				if parsedGen == "" {
					// Could not infer genus from tokens
					fmt.Fprintf(os.Stderr, "error: cannot infer genus from HUD line %q\n", strings.TrimSpace(firstLine))
					fmt.Fprintf(os.Stderr, "       expected format: '<Persona> <Genus>,' where genus is one of: %s\n", strings.Join(validGeneraList(cfg), ", "))
					fmt.Fprintf(os.Stderr, "       or use model names like haiku, sonnet, opus (auto-infers claude genus)\n")
					os.Exit(1)
				}
				if parsedMod != "" && parsedGen != "" {
					*mod = parsedMod
					*gen = parsedGen
					modelOverride = parsedModel
					if modelOverride != "" {
						aimux.Debug("HUD mode: parsed %s %s (model=%s) from first line", parsedMod, parsedGen, modelOverride)
					} else {
						aimux.Debug("HUD mode: parsed %s %s from first line", parsedMod, parsedGen)
					}
				}
			}

			// Create reader that replays first line then continues with rest of stdin
			// This allows genus to control how/when to consume stdin (streaming)
			stdinReader = io.MultiReader(strings.NewReader(firstLine), reader)
			aimux.Debug("Peeked first line (%d bytes), created replay reader", len(firstLine))
		}
	}

	modFlagProvided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "mod" {
			modFlagProvided = true
		}
	})

	// Apply environment defaults only if not already set by flags or HUD mode
	if *gen == "" {
		if envGen := os.Getenv("AIGEN"); envGen != "" {
			*gen = envGen
		} else {
			*gen = "bash"
		}
	}

	if !modFlagProvided && *mod == "" {
		if envMod := os.Getenv("AIMOD"); envMod != "" {
			*mod = envMod
		}
	} else if modFlagProvided && *mod == "" && os.Getenv("AIMOD") != "" {
		aimux.Debug("Explicitly overriding AIMOD=%s with -mod= (calling coordinator)", os.Getenv("AIMOD"))
	}

	if *cid == "" {
		*cid = os.Getenv("AICID")
	}
	if *cid != "" {
		*cid = aimux.NormalizeUUID(*cid)
	}
	if !*new {
		*new = os.Getenv("AINEW") != ""
	}

	if _, ok := cfg.GetGenus(*gen); !ok {
		fmt.Fprintf(os.Stderr, "error: invalid gen '%s' (valid: %s)\n", *gen, strings.Join(validGeneraList(cfg), ", "))
		os.Exit(1)
	}

	// Validate that at least one input is provided
	// BUT: if stdin is a pipe and we have a CID, allow empty cmdArgs (stdin will be consumed by genus)
	aimux.Debug("Validation: cmdArgs=%q stdinReader=%v cid=%q stdinIsPipe=%v", cmdArgs, stdinReader != nil, *cid, stdinIsPipe)
	if cmdArgs == "" && stdinReader == nil && !(*cid != "" && stdinIsPipe) {
		fmt.Fprintln(os.Stderr, "error: no prompt provided")
		flag.Usage()
		os.Exit(1)
	}

	var ctx *aimux.Context
	var err error

	if *cid == "" {
		// No CID provided - require explicit -new flag to create conversation
		if !*new {
			fmt.Fprintf(os.Stderr, "error: must specify -cid to resume or -new to create\n")
			fmt.Fprintf(os.Stderr, "usage: aimux -new <prompt>           # create new conversation\n")
			fmt.Fprintf(os.Stderr, "       aimux -cid=<uuid> <prompt>   # resume conversation\n")
			fmt.Fprintf(os.Stderr, "       aimux -cid=<uuid> -new ...   # branch from conversation\n")
			os.Exit(1)
		}
		ctx, err = aimux.InitContext(*gen, *mod)
		if err != nil {
			fmt.Fprintf(os.Stderr, "initialize context: %v\n", err)
			os.Exit(1)
		}
	} else {
		// CID provided - resume or branch from existing conversation
		ctx, err = aimux.ResumeContext(aimux.ID(*cid), *gen, *mod)
		if err != nil {
			// If both -new and -cid are provided, user is trying to branch
			if *new {
				fmt.Fprintf(os.Stderr, "error: cannot branch from non-existent conversation %s\n", *cid)
				fmt.Fprintf(os.Stderr, "       use -new alone to create new conversation\n")
				fmt.Fprintf(os.Stderr, "       or use -cid=<existing-uuid> -new to branch\n")
			} else {
				fmt.Fprintf(os.Stderr, "error: conversation %s not found\n", *cid)
			}
			os.Exit(1)
		}

		// If -new flag provided with CID, branch the conversation
		if *new {
			if err := aimux.Branch(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "branch session: %v\n", err)
				os.Exit(1)
			}
		}
	}

	ctx.WTF = *wtf

	// Apply CLI overrides for Context fields
	if *sid != "" {
		ctx.SID = aimux.ID(aimux.NormalizeUUID(*sid))
	}
	if *lvl >= 0 {
		ctx.LVL = *lvl
	}
	if *top != "" {
		ctx.TOP = *top
	}
	if *tag != "" {
		ctx.TAG = *tag
	}

	// Handle temporal rewind if requested
	if *rwd != "" {
		// Parse the timestamp
		cutoff, err := time.Parse(time.RFC3339, *rwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid rewind timestamp (use RFC3339 format): %v\n", err)
			os.Exit(1)
		}
		ctx.ENV["AIRWD"] = cutoff.Format(time.RFC3339)
		ctx.ENV["AITEMPORAL"] = "query"
		aimux.Debug("Temporal rewind to: %s", cutoff.Format(time.RFC3339))
	}

	// Handle custom system prompt if provided
	if *sys != "" {
		ctx.ENV["AISYS"] = *sys
	}

	// Handle model override from HUD parsing
	if modelOverride != "" {
		ctx.ENV["AIMODEL"] = modelOverride
		aimux.Debug("Model override from HUD: %s", modelOverride)
	}

	// Log user message (cmdArgs only - can't log streamed stdin without consuming it)
	loggedPrompt := cmdArgs
	if stdinReader != nil && cmdArgs != "" {
		loggedPrompt = cmdArgs + " <<STDIN"
	} else if stdinReader != nil {
		loggedPrompt = "<<STDIN"
	}

	// Infer organic flow hints from prompt patterns
	flowHints := aimux.InferFlowHints(loggedPrompt)
	for k, v := range flowHints {
		ctx.ENV["AI"+k] = v
		aimux.Debug("Flow hint: AI%s=%s", k, v)
	}

	if err := aimux.AppendMessage(ctx, "user", loggedPrompt); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log user message: %v\n", err)
	}

	// Start timing
	startTime := time.Now()

	// Call genus CLI with streaming (pass cmdArgs and stdin reader separately)
	stream, err := aimux.CallGenus(context.Background(), ctx, cmdArgs, stdinReader)
	if err != nil {
		handleError(ctx, err, "call genus")
	}

	// Stream and log the response
	if err := aimux.StreamAndLog(ctx, stream, os.Stdout); err != nil {
		stream.Close() // Clean up on error
		handleError(ctx, err, "stream")
	}

	// Close and check for blocking errors from subprocess exit code
	if err := stream.Close(); err != nil {
		handleError(ctx, err, "subprocess")
	}

	// Calculate elapsed time
	elapsed := time.Since(startTime).Round(time.Millisecond)

	// Print session info to stderr
	// Format: "Architect Claude / <elapsed> / <cid>" or with SID if different
	if ctx.CID == ctx.SID {
		fmt.Fprintf(os.Stderr, "\n\n%s / %s / %s\n", aimux.SigTag(ctx), elapsed, ctx.CID)
	} else {
		fmt.Fprintf(os.Stderr, "\n\n%s / %s / %s (%s)\n", aimux.SigTag(ctx), elapsed, ctx.CID, ctx.SID)
	}
}
