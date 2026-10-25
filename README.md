# prop-review

[日本語版 README](README.ja.md)

A local CLI for reviewing proposals item by item. Source code lives in `src/`. CLI text is in English; proposal topics follow the request's language.

## Install

Run from this repository's root and add `~/.local/bin` to your `PATH`.

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.pi/agent/skills"
go build -o "$HOME/.local/bin/prop-review" ./src
# Short alias (do not overwrite an existing prev command)
if ! command -v prev >/dev/null 2>&1 && [ ! -e "$HOME/.local/bin/prev" ] && [ ! -L "$HOME/.local/bin/prev" ]; then
  ln -s prop-review "$HOME/.local/bin/prev"
fi
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.pi/agent/skills/$skill" ] && [ ! -L "$HOME/.pi/agent/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.pi/agent/skills/$skill"
  fi
done
```

For other agents using the shared `~/.agents/skills` directory, link the skills there too:

```sh
mkdir -p "$HOME/.agents/skills"
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.agents/skills/$skill" ] && [ ! -L "$HOME/.agents/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.agents/skills/$skill"
  fi
done
```

Existing links are not overwritten. Run `/reload` if Pi is already open. Re-run `go build` after updating the CLI source.

## Commands

Run inside a Git repository. Displayed paths are abbreviated within the repository: the common `prop-review-tmp/` prefix is omitted, so a proposal appears as `review.xxx/proposal.txt`. The actual file remains under `prop-review-tmp/`; include that prefix when passing a path back to the CLI. `prev` is a shorter name accepting the same commands and arguments as `prop-review` (for example, `prev spec`). `generate`, `issue`, `revise`, `clarify`, `spec`, and `ingest` use its `prop-review-tmp/` directory.

### `prop-review` / `prop-review proposal.txt [result.txt]`

Opens the only unreviewed proposal, or lists unreviewed proposals to select from. You can specify a file instead:

```sh
prop-review
prop-review prop-review-tmp/review.123/proposal.txt
prop-review proposal.txt /path/to/result.txt
```

Enter `a` then Enter to approve, `c` then Enter to comment, Enter alone to leave unconfirmed, `b` then Enter to go back, or `q` then Enter to quit without saving. Fullwidth Latin keys work too. An empty comment warns that the item remains unconfirmed (Enter to confirm, Esc to return). Request and comment entry support Left/Right, Home/End, and Backspace; comments are limited to 1 MiB. Results go beside the proposal as `proposal.review.txt` by default and their path is printed. Existing results are never overwritten. A custom result path works for review, but cannot be used in `revise` or `spec` history.

### `prop-review ingest [NAME]`

```sh
prop-review ingest          # Start the configured engine (pi by default)
prop-review ingest claude   # Override for this launch; also fx, codex, agy, opencode
```

Start an interactive agent in this Git repository and ask it to follow `prop-review-ingest`: select the latest completed review by `proposal.txt` modification time, validate its history, report the result, and answer comments in chat. If no completed review exists, the agent asks for a result path. Neither the CLI nor the skill edits review files or starts implementation. Install and authenticate the agent CLI first. The skill must be available at `~/.agents/skills/prop-review-ingest/SKILL.md` or `~/.pi/agent/skills/prop-review-ingest/SKILL.md` (the Pi setup above installs the latter); other agents are given its path explicitly. `fx` has no interactive initial-prompt option: it runs `fx ask --prompt-permissions` to save the first turn, then `fx --continue` to enter that session. Agent-specific settings and tool permissions still apply; `ingest` does not sandbox agents.

### `prop-review generate`

```sh
prop-review generate
```

Enter a request to generate proposals and begin reviewing immediately. Requests accept up to 256 KiB of UTF-8, with surrounding whitespace trimmed. The root's `request.txt` stores the request. If review is cancelled, the generated unreviewed proposal remains.

### `prop-review issue`

```sh
prop-review issue
```

Select an open issue in a GitHub repository recognized by an authenticated `gh` CLI, generate proposals, and review them. The menu shows the first 30 issues by default (`gh issue list`). The engine receives the title, body, number, and URL; the title is stored in `request.txt`. Issue JSON larger than 256 KiB is rejected. The issue itself is not changed.

### `prop-review revise [proposal.txt]`

```sh
prop-review revise
prop-review revise prop-review-tmp/review.123/proposal.txt
prop-review  # Review the revision separately
```

Select a reviewed proposal with comments, or specify a proposal in the same repository. Only commented items are revised; pending items are carried forward unchanged, while approvals remain in the earlier review. A new `review.*` directory links back via `parent.txt`. **Revision does not start review automatically.** Pending-only reviews cannot be revised. Missing or mismatched adjacent results are rejected.

### `prop-review clarify`

```sh
prop-review clarify
```

Select a root request or issue title, validate that chain's approved, pending, and commented review history, and look for additional questions. Pending items carry forward. When questions remain, the CLI creates a linked proposal with `followup.txt` and `parent.txt` and starts review; when none remain, it saves nothing. Legacy roots without `request.txt`, missing or invalid results, and ambiguous histories fail. It recommends `revise` for comments and another review for pending items.

### `prop-review spec`

```sh
prop-review spec
```

Without a selection, validate **all reviewed chains** in this repository and send only approved topics to the engine and save a readable summary memo in `prop-review-tmp/spec.md`. Verbatim topics and source paths are not required. Generated summaries may misstate or add details; check against the original review results. Additional or pending questions do not block the confirmation step; `prop-review clarify` is recommended. Legacy roots lacking `request.txt` produce a warning because they cannot be clarified. One spinner updates throughout clarification without adding a line for each chain; no separate summary-generation spinner is shown. The full generated Markdown and destination are shown in the terminal. Creating or overwriting requires `y/N` confirmation: only `y` (or `Y`) proceeds; Enter and all other answers cancel. A chain with invalid history is excluded in full; valid chains can still be summarized. The preview warns about excluded chains and failed clarification checks before confirmation. No file is saved if no valid approved item remains. `spec.md` is a reference memo, not a formal specification or approval record. This does not start implementation or another review.

### `prop-review delete`

```sh
prop-review delete
```

Choose among unreviewed proposals and reviewed proposals with comments but no child revision. Before deleting, the CLI displays the complete proposal and review (including approved items); confirm with `y/N`. Only `y` (or `Y`) deletes; Enter and all other answers cancel. Directories referenced by `parent.txt`, or candidates with links or unexpected files, are refused. Confirmation **permanently deletes** the selected `review.*` directory.

### `prop-review engine` / `prop-review --engine NAME`

```sh
prop-review engine       # Print current engine
prop-review --engine pi  # Also accepts fx, claude, codex, agy, opencode
```

The engine setting is per user (`prop-review/engine` in the OS user config directory), defaults to `pi`, and applies across repositories. Install and authenticate the chosen CLI. Proposal generation uses Node.js and the global npm package `@earendil-works/pi-coding-agent` via its SDK, not the `pi` CLI; `ingest` starts the selected agent's interactive CLI.

## File format and review history

`proposal.txt` contains a single-line topic followed by an empty `Approval/Comment:` field for each item. Topics follow the request's language. Example (not built-in commands):

```text
Add a command to export the current settings
Approval/Comment:

Document how to restore a saved review
Approval/Comment:
```

Result fields accept only an empty value (pending), `Approved`, or `Comment: "..."` with Go-style quoting/escaping. Topics must be nonempty single lines; invalid UTF-8, the replacement character `�`, control characters, and duplicate text within one proposal are rejected. This also applies to generated output, saving, reading old proposals, and validating review history; existing files are not silently modified. The same topic may appear across generations when an item is carried forward in a revision. `parent.txt` records the previous `review.*` directory name; `followup.txt` marks a clarification proposal. Unlinked old revisions are not assumed to belong to the same chain.

`generate` and `revise` create unique `review.*` directories and add `/prop-review-tmp/` to local Git `info/exclude` if necessary. Reviewing alone does not change Git settings. Local review files may contain private requests and comments.

## Engines and failures

Engines are asked to read the repository and propose changes (or summarize approved topics for `spec`), not implement them. Responses are validated as JSON arrays of 1–10 single-line proposals; only `clarify` also allows an empty array. Language is not programmatically validated. An activity-specific spinner is shown on stderr, with a 100-second timeout. External CLI stdout, Codex result files, and OpenCode event streams are capped at 16 MiB each; stderr at 64 KiB; `npm root -g` stdout at 8 KiB; and assembled OpenCode text at 12 MiB. Exceeding a stream limit stops the process during collection and returns an explicit error. Invalid responses, failures, and cancelled input never overwrite earlier files.

Pi uses a read-only session with extensions, skills, and prompt templates disabled; Claude uses restricted mode with Read/Glob/Grep; Codex uses a read-only sandbox; `agy` uses plan mode and terminal sandboxing; OpenCode uses a dedicated agent restricted to read/glob/grep. `agy` may save local session data. OpenCode passes prompts as command arguments and rejects those over 100,000 bytes. Instructions to `fx ask --no-save` cannot enforce tool permissions. Local settings, hooks, and plugins of external CLIs may have side effects.

Use the `prop-review-ingest` skill to bring completed reviews into chat and answer comments. The `prop-review` skill handles generation and review handoff. Approval applies only to that item; implementation does not start automatically.
