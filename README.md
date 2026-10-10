# prop-review

A local CLI for reviewing proposals item by item. It is separate from zintent's intent-approval gate. Source code lives in `src/`.

## Install (available in any repository)

Run from this repository's root. Add `~/.local/bin` to your `PATH`.

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.pi/agent/skills"
go build -o "$HOME/.local/bin/prop-review" ./src
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.pi/agent/skills/$skill" ] && [ ! -L "$HOME/.pi/agent/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.pi/agent/skills/$skill"
  fi
done
```

For agents other than Pi that support the shared `~/.agents/skills` directory, link the skills there too:

```sh
mkdir -p "$HOME/.agents/skills"
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.agents/skills/$skill" ] && [ ! -L "$HOME/.agents/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.agents/skills/$skill"
  fi
done
```

Existing destinations are not overwritten. Check their targets if necessary. Run `/reload` if Pi is already open.

## Review

```sh
prop-review
```

From any directory inside a Git repository, this opens the only unreviewed `prop-review-tmp/review.*/proposal.txt` under the repository root, or lists unreviewed proposals for you to select when there are several. The list is ordered by proposal modification time (then path, descending). Enter a listed number or `q` to cancel. You can also supply a path: `prop-review proposal.txt`. Earlier proposals and results are never overwritten or deleted. `generate` and `revise` create a unique directory and add `/prop-review-tmp/` to the repository's local Git `info/exclude` if needed; plain review does not change Git settings.

The proposal format has two lines per item, with an empty review field; generated files separate items with a blank line. `Approval/Comment:` is a fixed file-format token, not UI language; proposal text can be in the request's language. These are illustrative proposals, not built-in actions:

```text
Add a command to export the current settings
Approval/Comment:

Document how to restore a saved review
Approval/Comment:
```

Only the `Approval/Comment:` field is accepted in proposals; topic text is not interpreted as a format prefix. Proposal topics must be nonempty single lines without invalid UTF-8, the replacement character `�`, or control characters.

Enter `a` then Enter to approve, `c` then Enter to enter a comment, Enter alone to leave an item unconfirmed, `b` then Enter to go back, or `q` then Enter to quit without saving. Fullwidth Latin letter keys also work. Each menu choice is confirmed with Enter so its newline cannot skip the next item. Request and comment input support Left/Right, Home/End, and Backspace (including multibyte characters). Comments accept up to 1 MiB of UTF-8 text; exceeding the limit rejects that entry instead of silently truncating it. If a comment is empty, the CLI warns that it leaves the item unconfirmed (Enter to confirm, Esc to go back).

By default, results are written to `proposal.review.txt` next to the proposal, and the path is printed. Existing results are not overwritten. To keep the result elsewhere, run `prop-review proposal.txt /path/to/result.txt`. Interrupting review saves nothing. The file-format values `Approved` and `Comment: "..."` distinguish approvals and comments; comments are quoted/escaped in the file and decoded when read. Review results use only `Approval/Comment:` with an empty value, `Approved`, or `Comment: "..."`; other formats are rejected.

## Generate and revise (fx or Pi SDK)

For fx, install and authenticate the `fx` CLI. For Pi, install Node.js and the global npm package `@earendil-works/pi-coding-agent` and configure its credentials and model. Pi generation uses its SDK, **not** the `pi` CLI. The selected engine is a user-wide setting shared across repositories (fx by default). Neither libfx nor a direct model API integration is used.

```sh
prop-review engine       # Show current engine (fx by default; does not change settings)
prop-review --engine pi  # Use Pi from now on
prop-review generate     # Enter a request, generate proposals, then review
prop-review issue        # Select an open GitHub issue, generate proposals, then review
prop-review revise       # Select from reviewed proposals awaiting revision; save a new proposal.txt
prop-review revise prop-review-tmp/review.123/proposal.txt  # Or specify one directly
prop-review              # Review the revised proposal
prop-review --engine fx  # Switch back to fx
```

The engine setting is stored at `prop-review/engine` in the OS user config directory (`os.UserConfigDir()`). Run `generate` and `revise` in a repository terminal.

`issue` requires an installed, authenticated `gh` CLI and a GitHub repository recognized by `gh`. It lists the first 30 open issues (the `gh issue list` default), prompts for a listed issue number in the terminal (`q` cancels), and sends its title, body, number and URL to the selected engine. Issues outside that list are not selectable. Oversized issue data (>256 KiB as JSON) is rejected. Issue text is treated as untrusted input; no issue is changed by the CLI. As with `generate`, a successful generation creates a new proposal and starts review.

Both engines are prompted to inspect the repository and propose changes, not implement them. The initial request accepts up to 256 KiB of UTF-8 text; leading/trailing whitespace is trimmed before it is included in the prompt sent via stdin. Engines are instructed to use the request's language for new proposals and the original topics' language for revisions, even if comments are in another language; language is not programmatically validated. Pi uses an in-memory session with extensions, skills, and prompt templates disabled, and only read-only tools. The external `fx ask --no-save` process is prompted not to change files, but the CLI cannot enforce its tool permissions. While waiting for either engine, elapsed seconds are displayed on stderr; generation times out after 100 seconds without saving a new proposal.

Responses are validated as a JSON array of 1–10 nonempty single-line proposals and converted to `proposal.txt` with empty review fields. With no path, `revise` uses the only reviewed proposal with comments and no child revision, or lists such proposals for selection if there are several (newest proposal modification time, then path). Pass a relative or absolute `prop-review-tmp/review.*/proposal.txt` from the same repository to revise that exact reviewed proposal instead; missing or mismatched adjacent results fail without falling back to another proposal. Revisions replace only commented items; unconfirmed items remain, and approved items remain in the earlier proposal and result. Each revised proposal includes a `parent.txt` file containing the previous `review.*` directory name, linking its review history. The `prop-review-ingest` skill follows these links to report approvals from earlier rounds together with the latest results. Pre-existing revisions without a link cannot be joined reliably. A revision does not automatically start review. Failed generation, invalid output, and cancelled input do not start review or overwrite earlier files. If generation succeeds but review is cancelled, the new unreviewed proposal remains on disk. Reviews saved to a custom destination instead of the default `proposal.review.txt` cannot be revised with `revise`.

Use the `prop-review-ingest` skill to bring completed results into chat and receive the agent's response to the review, including answers to comments when possible. The `prop-review` skill handles generation and review handoff, not ingestion. Approval concerns only the specific item; it never automatically starts implementation.
