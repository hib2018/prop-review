---
name: prop-review
description: Create a proposal.txt of individually reviewable options or next steps from an initial idea or request; then hand off to prop-review for human review. Use for proposal generation, direction or issue analysis with possible next steps, or an explicit request to revise proposals. Do not use to read or ingest completed review results; use prop-review-ingest instead.
---

# prop-review: request to reviewable proposals

Start from the human's own words. Write proposal text and the human-facing handoff in the language of the human's request, even when repository docs, CLI UI, or skill instructions use English. On revision, preserve the original request language even if comments use another language. This is a lightweight discussion, not an approval gate for a whole project. Use it for early ideas and direction-setting, but also for other requests when the human wants to review possible choices or next steps. Do not turn a request for facts only or direct implementation into an approval session without being asked.

1. If the request lacks the minimum context to propose anything concrete, ask one short question. Otherwise write up to 10 small, independently reviewable items (around 10 when warranted; fewer rather than padding). Inspect the relevant project when proposals depend on its actual state. Convert findings into specific proposed responses rather than asking the human to approve a fact; distinguish confirmed problems from optional improvements. Make assumptions explicit in each item; do not invent requirements or silently expand scope.
2. When the human wants CLI generation, ask them to run `prop-review generate` in the repository; it asks `Request:`, invokes the user-selected CLI (default fx) to read the repository, creates a new proposal and starts review. The human can switch the user-wide engine beforehand with `prop-review --engine pi` or `prop-review --engine fx`. Do not run the interactive CLI on the human's behalf. For manually drafted items, find the repository root with `git rev-parse --show-toplevel`. Create `<repo-root>/prop-review-tmp/`, keep it out of Git by adding `/prop-review-tmp/` to that repository's local `info/exclude` (`git rev-parse --git-path info/exclude`) if not already ignored. Never delete earlier `review.*` directories. Create a unique directory with `mktemp -d <repo-root>/prop-review-tmp/review.XXXXXX` and write `proposal.txt` there (blank approval lines, no other headings). `承認/コメント：` is a fixed file-format token, not prose to translate; leave it empty in proposals. If there is no repository, ask where to save:

   ```text
   <one concrete proposal>
   承認/コメント：

   <another proposal>
   承認/コメント：
   ```

3. For manually drafted items, show them to the human and give the command `prop-review` to run from anywhere inside that repository. It opens the latest unreviewed proposal under `prop-review-tmp/` by proposal modification time (then path). By default the result is saved beside the proposal as `proposal.review.txt`. Reviewing alone does not add a Git exclusion; ensure manually drafted proposals are ignored as described above. Proposals and results remain on disk until explicitly deleted; they are not promoted to durable project artifacts automatically. To save the result somewhere durable, the human can specify an explicit proposal and destination: `prop-review <absolute-path-to-proposal.txt> <absolute-path-to-result.txt>`; do not silently persist proposals or results elsewhere. The CLI needs the human's terminal; do not run it from a non-interactive agent shell or answer on the human's behalf. Stop after handing off the review; do not read or report its result in this skill. For a later request to bring results into chat, use `prop-review-ingest`.
4. Only when explicitly requested to revise, ask the human to run `prop-review revise` (using the selected engine): it uses the latest reviewed proposal with a default adjacent `proposal.review.txt`, replaces commented items, preserves unconfirmed items, omits approved items from the new proposal, and does not automatically open review. Results saved only to a custom path cannot be used by `revise`. Do not read the result in this skill.

Approval applies only to the specific item. An empty answer is not approval. Never start implementation, convert to zintent approval, or mutate Spec Kit artifacts solely because an item says `承認`; wait for an explicit human request. If `prop-review` is unavailable, say so and provide the two-line template for manual review instead.
