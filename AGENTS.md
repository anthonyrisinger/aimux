# AI Multiplexer (AIMUX) Constitution

The purpose of this constitution is to provide a stable foundation for the ongoing development of the AI orchestration framework. It codifies the project’s mission, values, governance model, contributor roles and technical standards. All contributors and maintainers agree to abide by these rules when proposing, reviewing and merging changes.

## 1. Mission and Scope

1.1 **Mission:** To build and maintain a portable, extensible, and open‑source framework for orchestrating interactions with multiple AI models while preserving conversation context, enabling safe partner‑protocol compliance, and facilitating future features such as tagging, asynchronous messaging and adaptive parameter control.

1.2 **Scope:** The framework focuses on command‑line and library functionality for session management, prompt construction, model dispatch, logging and eventual compaction. It does not prescribe particular AI providers; support for new genera may be added as modules within this framework.

1.3 **Sustainability:** The project aims to remain genus‑agnostic, lightweight, and maintainable. Code should be clear and easy to audit; dependencies should be kept to a minimum; and breaking changes should be carefully managed.

## 2. Core Principles

2.1 **Clarity Over Cleverness:** Prefer explicit, simple code to opaque one‑liners. Documentation must accompany non‑obvious logic. Short variable names are allowed where context makes them clear; avoid ambiguous abbreviations.

2.2 **State Is Sacred:** Conversation state (conversation IDs, session IDs, top/callee tags, call depth and logs) is the heart of the framework. Changes to how state is represented or persisted require consensus and a version bump.

2.3 **Extensibility:** Design APIs and data structures so that new features—such as tagging, messaging queues, or model parameters—can be added without breaking existing functionality. Use maps or optional fields rather than proliferating bespoke variables.

2.4 **Portability:** The codebase should remain platform‑agnostic. File paths, logging and time handling should use Go’s standard library abstractions.

2.5 **Resumability:** Every interaction with the framework should leave behind sufficient state (metadata and logs) to resume the conversation later without loss of context.

2.6 **Partner‑Protocol Fidelity:** The semantics of the original shell’s partner protocol—including safe call depth, recursion avoidance and persona signatures—must be preserved. New features should respect these semantics.

## 3. Governance

3.1 **Maintainers:** A small group of maintainers oversees the project. They have commit access and are responsible for reviewing pull requests, ensuring compliance with this constitution, and performing releases.

3.2 **Contributors:** Anyone is welcome to contribute via pull requests. Contributors should read this constitution, follow the development guidelines in `README.md` and adhere to code review feedback. Contributions are credited in release notes.

3.3 **Decision Making:** Major decisions (e.g. adding support for a new genus, introducing a new dependency, changing the data model, or modifying the constitution itself) require consensus among the maintainers. Minor bug fixes or improvements may be merged by a single maintainer after at least one approving review.

## 4. Development Workflow

4.1 **Branching Model:** Use feature branches for all changes. The `main` branch contains the latest stable code. For releases, create a `vX.Y` branch if needed to backport bug fixes.

4.2 **Commit Messages:** Use low-effort conventional commit format (e.g. `feat: add -new flag`, `fix: handle missing session gracefully`). Include a short summary and a longer body explaining why the change is needed.

4.3 **Testing:** All non‑trivial code must be accompanied by tests. Tests should be deterministic and should not rely on external services. Mock model dispatches if necessary.

4.4 **Code Style:** Use `go fmt` for formatting. Avoid complex generic constructs unless absolutely necessary. Document exported functions and types using Go doc conventions. Flag unused code for removal during review.

4.5 **Documentation:** When adding or changing functionality, update `README.md`, this constitution, or other documentation as appropriate. If the change involves a new flag or field, describe its purpose and usage.

## 5. Versioning and Releases

5.1 **Semantic Versioning:** The project follows semantic versioning (`MAJOR.MINOR.PATCH`). Increment the **major** version when making incompatible API changes; **minor** version when adding functionality in a backwards compatible manner; and **patch** version for backwards compatible bug fixes.

5.2 **Release Process:** A maintainer drafts release notes summarising new features, fixes and breaking changes. Tags are created on the `main` branch. Binary builds may be attached to release artifacts.

5.3 **Deprecation Policy:** When functionality is slated for removal, mark it as deprecated in the code and documentation at least one minor release in advance, with clear guidance on alternatives.

## 6. Adding New Models and Features

6.1 **Model Integration:** Adding a new AI genus or persona should not require modifying the `Context` struct. Instead, extend the model dispatch layer (`CallModel`) or create new dispatcher functions. Use configuration files or environment variables to register new genera.

6.2 **Tagging and Asynchronous Messaging:** When implementing tagging or messaging, use structured metadata (JSON or similar) stored alongside session logs. Do not break existing log formats. Define clear interfaces (e.g. `Tagger`, `Messenger`) and provide reference implementations.

6.3 **Adaptive Controls:** Features such as automatic temperature tuning or context compaction should be introduced behind feature flags or configuration settings. They must log their actions and expose metrics for debugging.

6.4 **Security and Privacy:** Any feature that touches external services (e.g. model APIs) must handle secrets securely. Do not commit API keys. If storing sensitive data, encrypt it or provide guidance for secure storage. Do not log user prompts or model responses in plaintext when security is a concern.

## 7. Changes to This Constitution

Amendments to this constitution require a pull request with clear rationale. The proposal must be approved by all maintainers and remain open for community feedback for at least two weeks before merging. Exceptions may be made for urgent security or legal issues.
