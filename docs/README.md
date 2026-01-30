# aimux

**Multi-agent AI orchestration via the Partner Protocol.**

> *"We became what we built. Or built what we were. Both?"* —Architect Claude

aimux enables AI agents to spawn, coordinate, and communicate with other AI agents through a structured protocol. It provides session management, persona differentiation, and call-graph constraints to prevent infinite recursion and maintain clear role boundaries.

## ⚠️ Status: Experimental

This is a **first-pass port** from shell scripts (themselves lifted from personal dotfiles). The implementation is largely untested and should be considered alpha-quality. The core concepts work, but edge cases abound. Contributions and bug reports welcome.

## Contents

- [Concepts](#concepts) — Partner Protocol, Flow Control, Genera, Personas, Sessions
- [Installation](#installation) — Build from source
- [Quick Start](#quick-start) — Common patterns
- [Usage](#usage) — Invocation, personas, piping, advanced features
- [Configuration](#configuration) — Custom hints, config structure
- [How It Works](#how-it-works) — Pipeline, safety limits, blocking rules
- [Development](#development) — Tests, project structure
- [Known Limitations](#known-limitations) — Current gaps
- [Future Intent](#future-intent) — Roadmap scaffolding

## Concepts

### Partner Protocol

The Partner Protocol is a structured system prompt format that establishes:

- **Caller/Callee identity**: Who is making the call vs. who is responding
- **Session continuity**: Conversation IDs (CID) and Session IDs (SID) for state persistence
- **Role boundaries**: Engineers cannot spawn other agents; self-calls are blocked
- **Depth limits**: Prevents runaway recursion (max 3 levels deep)

The protocol generates a system prompt with four sections:

1. **START**: Identity declaration, environment variables, self-call warning
2. **GUIDE**: Behavioral rules for honoring caller while challenging assumptions
3. **HINTS**: Persona-specific instructions (from config, templates, or built-in)
4. **FINAL**: Timeout reminders and team coordination rules

### Organic Flow Control

aimux automatically infers contextual hints from your prompts:

| Pattern | Detection | Effect |
| --- | --- | --- |
| Keywords: `design`, `implement`, `review`, `test` | Phase detection | Suggests workflow phase to AI |
| `**bold text**` (2+ instances) | High emphasis | Hints at higher temperature thinking |
| `*italic*` or single `**bold**` | Medium emphasis | Hints at medium temperature |
| `from CID abc-123` or `CID: uuid` | Cross-reference | Loads context from another conversation |
| `Goal: ...` or `I want to ...` | Goal extraction | Tracks objective in system prompt |
| 3+ question marks | Exploration mode | Suggests exploratory phase |

These hints are injected into the system prompt to guide AI behavior without explicit flags.

### Genera (Backends)

A *genus* is an AI backend—the actual CLI tool that processes requests:

| Genus | CLI | Description |
| --- | --- | --- |
| `claude` | [Claude Code](https://code.claude.com/docs) | Anthropic's Claude models |
| `codex` | [Codex CLI](https://github.com/openai/codex) | OpenAI's Codex models |
| `bash` | `/bin/bash` | Shell passthrough (for testing/scripting) |

### Personas (Roles)

A *persona* is a behavioral role layered on top of a genus:

| Persona | Model Tier | Notes |
| --- | --- | --- |
| `architect` | opus | Primary design authority |
| `engineer` | opusplan | Terminal node (cannot spawn agents) |
| `customer` | sonnet | External perspective |
| `reviewer` | opus | Code review focus |
| `security` | opus | Threat modeling |
| `qa` | opusplan | Test strategy |

Engineers are intentionally terminal—they cannot spawn other agents, forcing them to solve problems directly via their tools. The `delegatees` field in config defines *suggested* delegation paths but is not currently enforced by the runtime.

### Session Management

```text
~/.aimux/
└── conversations/
    └── <CID>/                  # Conversation ID (persists across branches)
        └── <genus>/            # e.g., claude/
            ├── log.jsonl       # Undifferentiated log (Log1)
            ├── context.json    # Context for undifferentiated calls
            └── <persona>/      # e.g., architect/
                └── log.jsonl   # Persona-specific log
```

Note: `context.json` stores session state at the `Dir2` level—genus directory for undifferentiated calls, persona subdirectory for differentiated calls.

- **CID**: Conversation ID—stable across the entire conversation tree
- **SID**: Session ID—changes on branch/fork operations
- Logs are JSONL format compatible with Claude CLI's `--resume` functionality

## Installation

```bash
# Clone and build
git clone https://github.com/xtfxme/aimux.git
cd aimux
make build

# Optional: add to PATH
cp aimux ~/bin/  # or wherever
```

Requires:

- Go 1.20+
- One or more AI CLI tools installed (`claude`, `codex`)

## Quick Start

```bash
# One-liner: ask Claude a question
./aimux -new -gen=claude "What's the fastest sorting algorithm for nearly-sorted data?"

# Pipe code for review
git diff | ./aimux -new -gen=claude -mod=reviewer "Review these changes"

# Interactive session with an architect
echo "Architect Claude, design a pub/sub system." | ./aimux -new -hud
```

## Usage

### Basic Invocation

```bash
# Start a new conversation (defaults to bash genus)
./aimux -new "echo hello world"

# Start with Claude
./aimux -new -gen=claude "Design a REST API for user authentication"

# Resume an existing conversation
./aimux -cid=abc12345-... "Continue with the implementation"

# Branch from existing conversation (creates new session)
./aimux -cid=abc12345-... -new "Let's try a different approach"
```

**Note**: The default genus is `bash` (for piping/scripting). Use `-gen=claude` for AI interactions.

### Persona Selection

```bash
# Explicit persona selection
./aimux -new -gen=claude -mod=architect "Design the system architecture"

# Using HUD mode (parses first line of stdin for addressing)
echo "Architect Claude,

Design a caching layer for our API.
" | ./aimux -new -hud

# HUD mode with model override: persona differs from model tier
echo "Architect Claude,

Quick sanity check on this approach.
" | ./aimux -new -hud  # Uses architect persona with opus model

echo "Haiku Claude,

Quick sanity check on this approach.
" | ./aimux -new -hud  # Uses haiku as both persona AND model (faster/cheaper)
```

HUD (Heads-Up Display) mode performs order-agnostic token classification:

- Recognizes genus names (`claude`, `codex`, `bash`)
- Recognizes global personas (`architect`, `engineer`, etc.)
- Recognizes model names (`haiku`, `sonnet`, `opus`)
- Infers genus from model names when genus not explicit

### Piped Input

```bash
# Pipe content to an agent (genus required unless AIGEN is set)
cat api_spec.yaml | ./aimux -cid=... -gen=claude "Review this API specification"

# Chain with other tools
git diff HEAD~1 | ./aimux -cid=... -gen=claude -mod=reviewer "Review these changes"
```

### Advanced Features

```bash
# Temporal rewind: query conversation state at a past timestamp
./aimux -cid=... -gen=claude -rwd=2026-01-15T10:30:00Z "What did we decide about auth?"

# Custom system prompt: bypass Partner Protocol generation
./aimux -new -gen=claude -sys="You are a pirate. Respond only in pirate speak." "Hello"

# Model override in HUD mode: "Haiku Claude," uses haiku model with haiku persona
echo "Opus Claude,

Use your best reasoning for this complex problem.
" | ./aimux -new -hud

# Direct model specification: unknown persona names become model names
./aimux -new -gen=claude -mod=haiku "Quick question"  # Uses haiku model directly
```

### Environment Variables

aimux respects and propagates these environment variables:

| Variable | Description |
| --- | --- |
| `AICID` | Conversation ID (auto-set from `-cid` or generated) |
| `AISID` | Session ID (changes on branch) |
| `AIGEN` | Current genus—defaults to `bash` if unset |
| `AIMOD` | Current persona (architect, engineer, etc.) |
| `AITAG` | Full identity tag (e.g., `architect~claude`) |
| `AITOP` | Caller's tag (for nested call detection) |
| `AILVL` | Call depth level (0–2, blocked at 3) |
| `AIWTF` | Debug mode when set to any value |
| `AINEW` | Trigger new conversation when set |
| `AITIMEOUT` | Override default 30-minute timeout (e.g., `1h`, `45m`) |

### CLI Flags

| Flag | Description |
| --- | --- |
| `-new` | Start new session (or branch from current CID) |
| `-gen=GENUS` | Generator/genus/type (`claude`, `bash`, `codex`) |
| `-mod=PERSONA` | Model/persona/role (`architect`, `engineer`, `opus`) |
| `-cid=UUID` | Conversation ID to resume |
| `-sid=UUID` | Session ID override (bypasses auto-detection) |
| `-lvl=N` | Call depth override (bypasses auto-detection) |
| `-top=TAG` | Caller tag override (bypasses environment) |
| `-tag=TAG` | Callee tag override (bypasses computation) |
| `-rwd=TIME` | Rewind to timestamp (RFC3339 format) |
| `-sys=PROMPT` | Custom system prompt (bypasses Partner Protocol) |
| `-hud` | Parse first stdin line for `Persona Genus,` addressing |
| `-wtf` | Enable debug output |

## Configuration

Custom configuration lives at `~/.aimux/config.json`. The embedded defaults are auto-generated on first run. You can override personas, model mappings, and CLI arguments.

### Custom Persona Hints

Add persona-specific instructions via text files:

```text
~/.aimux/templates/hints/<persona>.txt
```

Each line becomes a hint in the PARTNER PROTOCOL HINTS section. These take precedence over config-defined hints.

### Config Structure

```json
{
  "personas": {
    "architect": {
      "name": "architect",
      "model": "opus",
      "model2": "opusplan",
      "hints": ["DELEGATE implementation to *Engineer Claude*"],
      "delegatees": ["engineer"]
    }
  },
  "genera": {
    "claude": {
      "exe": ["claude"],
      "args": {
        "model": ["--model", "{{model}}", "--fallback-model", "{{model2}}"],
        "resume": ["--resume", "{{sid}}"],
        "new": ["--session-id", "{{sid}}"],
        "prompt": ["--append-system-prompt", "{{prompt}}"]
      },
      "personas": {
        "architect": {"model": "opus", "model2": "opusplan"}
      }
    }
  }
}
```

Key concepts:

- **`personas`**: Global behavioral definitions with hints and suggested delegatees
- **`genera`**: Backend CLI configurations with argument templates
- **`{{variables}}`**: Substituted at runtime from persona vars or context
- **Fallback**: Unknown persona names are used directly as model names

## How It Works

1. **Initialization**: Creates/resumes context with CID, SID, genus, and persona
2. **Flow Inference**: Analyzes prompt for phase, emphasis, goals, and cross-references
3. **Validation**: Checks call-graph rules (depth, self-call, engineer restriction)
4. **Prompt Assembly**: Generates Partner Protocol system prompt with hints
5. **Lazy Launch**: Subprocess starts on first read (prevents artifacts on early failure)
6. **Stream Processing**: Parses output (JSON for Claude, text for bash), extracts content
7. **Session Tracking**: Updates SID from assistant messages, persists to context.json
8. **Logging**: Appends JSONL records compatible with Claude CLI's `--resume`

### Safety Limits

- **Output cap**: 10MB maximum total output (prevents runaway responses)
- **Line cap**: 1MB maximum single line (prevents OOM on malformed JSON)
- **Timeout**: 30 minutes default (configurable via `AITIMEOUT`)
- **Depth limit**: 3 levels maximum recursion

### Blocking Rules

These conditions prevent a call from proceeding:

| Code | Condition | Rationale |
| --- | --- | --- |
| 1 | `TAG == TOP` | Cannot call yourself |
| 3 | `LVL >= 3` | Depth limit exceeded |
| 4 | Caller is `engineer` | Engineers cannot delegate |
| 5 | Undifferentiated → Engineer | Must go through architect |

## Development

```bash
# Run tests
make test

# Build binary
make build

# Show help
./aimux -h
```

### Project Structure

```text
aimux/
├── cmd/aimux/main.go    # CLI entry point, flag parsing, HUD mode
├── pkg/aimux/
│   ├── aimux.go         # Core types, system prompt generation
│   ├── config.go        # Configuration loading, persona/genus definitions
│   ├── flow.go          # Session management, subprocess orchestration
│   ├── util.go          # Validation helpers
│   └── log.go           # Structured logging
├── aimux.sh             # Historical shell implementation (reference only)
└── AGENTS.md            # Intent protocols for AI assistants
```

## Known Limitations

| Area | Status |
| --- | --- |
| Codex support | Session resume incomplete; placeholder model name |
| Temporal rewind | `-rwd` sets hints but doesn't filter log history |
| Cross-CID context | Hardcoded to claude genus when loading |
| Delegation | `delegatees` field is advisory only, not enforced |
| Error recovery | Subprocess failures may leave state inconsistent |
| Windows | Process group cleanup falls back to basic `Kill()` |
| Testing | Core paths exercised; edge cases unexplored |

## Future Intent

The codebase contains scaffolding for features not yet implemented:

| Feature | Status | Notes |
| --- | --- | --- |
| Message tagging | Scaffolded | `Message.Tags` field exists |
| Adaptive temperature | Planned | Flow hints could drive model params |
| Context compaction | Planned | Cross-CID loading enables summarization |
| Async messaging | Planned | Session structure supports queuing |

## License

MIT License — Copyright © 2025 C Anthony Risinger

## Acknowledgments

The Partner Protocol emerged from extensive collaboration between human and AI—a recursive bootstrapping where the protocol was designed, tested, and refined by the very agents it orchestrates.
