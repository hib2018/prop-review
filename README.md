# prop-review

The CLI source lives in `src/` (`commands.go`, `engine.go`, `proposal.go`, and `terminal.go`).

A small local CLI for approving or commenting on proposals item by item. It is separate from zintent's intent-approval gate.

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

From any directory inside a Git repository, this opens the newest unreviewed `prop-review-tmp/review.*/proposal.txt` under the repository root. You can also supply a path: `prop-review proposal.txt`. Earlier proposals and results are never overwritten or deleted. The CLI adds `/prop-review-tmp/` to the repository's local Git `info/exclude` if needed; it does not edit tracked files.

The proposal format has two lines per item, with an empty review field. The **field name is a legacy file-format token**, not UI text; keep it in Japanese for compatibility. Proposal text can be in the request's language:

```text
Update the configuration
承認/コメント：

Leave existing data unchanged
承認/コメント：
```

Legacy proposals prefixed with `何について：` are also accepted; the prefix is omitted from results. Either ASCII or fullwidth colons are accepted in input review fields. Invalid UTF-8, the replacement character `�`, and terminal control characters are rejected.

Enter `a` then Enter to approve, `c` then Enter to enter a comment, Enter alone to leave an item unconfirmed, `b` then Enter to go back, or `q` then Enter to quit without saving. Fullwidth Latin letter keys also work. Each menu choice is confirmed with Enter so its newline cannot skip the next item. In comments, use Left/Right arrows to move the cursor, Home/End to jump to the ends, and Backspace to delete before the cursor (including Japanese characters). Comments accept up to 1 MiB of UTF-8 text; exceeding the limit rejects that entry instead of silently truncating it. If a comment is empty, the CLI warns that it leaves the item unconfirmed (Enter to confirm, Esc to go back).

Results are written to `proposal.review.txt` next to the proposal, and the path is printed. Existing results are not overwritten. To keep the result elsewhere, run `prop-review proposal.txt /path/to/result.txt`. Interrupting review saves nothing. The file-format values `承認` and `コメント："..."` distinguish approvals and comments; comment text remains unchanged.

## Generate and revise (fx or Pi SDK)

For fx, install and authenticate the `fx` CLI. For Pi, install Node.js and the global npm package `@earendil-works/pi-coding-agent` and configure its credentials and model. Pi generation uses its SDK, **not** the `pi` CLI. The selected engine is a user-wide setting shared across repositories (fx by default). Neither libfx nor a direct model API integration is used.

```sh
prop-review --engine pi  # Use Pi from now on
prop-review generate     # Enter a request, generate proposals, then review
prop-review revise       # Revise commented items; only save a new proposal.txt
prop-review              # Review the revised proposal
prop-review --engine fx  # Switch back to fx
```

The engine setting is stored at `prop-review/engine` in the OS user config directory (`os.UserConfigDir()`). Run `generate` and `revise` in a repository terminal.

Both engines are instructed to inspect the repository and propose changes, never implement them or modify files. The initial request accepts up to 256 KiB of UTF-8 text and can be edited with the same cursor keys; the original request is passed as written (via stdin for either engine), and generated proposal text must use the **same language as that request**. Revisions must retain the original proposal language even when comments use another language. Pi uses an in-memory session with extensions, skills, and prompt templates disabled, and only read-only tools. While waiting for either engine, elapsed seconds are displayed on stderr; the operation times out after 100 seconds without creating a proposal file.

Responses are validated as a JSON array of 1–10 one-line proposals and converted to `proposal.txt` with empty review fields. Revisions replace only commented items; unconfirmed items remain, and approved items remain in the earlier proposal and result. A revision does not automatically start review. Failures, invalid output, and cancelled input do not start review or overwrite earlier files. Reviews saved to a custom destination instead of the default `proposal.review.txt` cannot be revised with `revise`.

Use the `prop-review-ingest` skill to bring completed results into chat and receive the agent's response to the review, including answers to comments when possible. The `prop-review` skill handles generation and review handoff, not ingestion. Approval concerns only the specific item; it never automatically starts implementation.
