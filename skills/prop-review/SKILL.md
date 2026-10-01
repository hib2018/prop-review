---
name: prop-review
description: Create a proposal.txt of individually reviewable options or next steps from an initial idea, a direction-setting request, or any other topic the human wants to discuss and review item by item; then collect approval or comments with prop-review. Use when the human asks for idea generation, direction or issue analysis with possible next steps, proposals, options, a lightweight review, or invokes prop-review. Not for ordinary information-only answers, zintent approval, Spec Kit definitions, or automatic implementation.
---

# prop-review: request to reviewable proposals

Start from the human's own words, in their language. This is a lightweight discussion, not an approval gate for a whole project. Use it for early ideas and direction-setting, but also for other requests when the human wants to review possible choices or next steps. Do not turn a request for facts only or direct implementation into an approval session without being asked.

1. If the request lacks the minimum context to propose anything concrete, ask one short question. Otherwise write 1–5 small, independently reviewable items. Inspect the relevant project when proposals depend on its actual state. Convert findings into specific proposed responses rather than asking the human to approve a fact; distinguish confirmed problems from optional improvements. Make assumptions explicit in each item; do not invent requirements or silently expand scope.
2. When the human wants CLI generation, ask them to run `prop-review generate` in the repository; it asks `依頼：`, invokes fx to read the repository, creates a new proposal and starts review. Do not run the interactive CLI on the human's behalf. For manually drafted items, find the repository root with `git rev-parse --show-toplevel`. Create `<repo-root>/prop-review-tmp/`, keep it out of Git by adding `/prop-review-tmp/` to that repository's local `info/exclude` (`git rev-parse --git-path info/exclude`) if not already ignored. Never delete earlier `review.*` directories. Create a unique directory with `mktemp -d <repo-root>/prop-review-tmp/review.XXXXXX` and write `proposal.txt` there (blank approval lines, no other headings). If there is no repository, ask where to save:

   ```text
   <one concrete proposal>
   承認/コメント：

   <another proposal>
   承認/コメント：
   ```

3. For manually drafted items, show them to the human and give the command `prop-review` to run from anywhere inside that repository. It automatically opens the latest unreviewed proposal under `prop-review-tmp/`. By default the result is saved beside the proposal as `proposal.review.txt`. Proposals and results remain on disk until explicitly deleted; they are not promoted to durable project artifacts automatically. To save the result somewhere durable, the human can specify an explicit proposal and destination: `prop-review <absolute-path-to-proposal.txt> <absolute-path-to-result.txt>`; do not silently persist proposals or results elsewhere. The CLI needs the human's terminal; do not run it from a non-interactive agent shell or answer on the human's behalf. Pause here until the human says they have finished reviewing or supplies the result.
4. Read the default `<absolute-path-to-proposal.review.txt>` or the human's chosen result path after the human confirms completion. Report which items were approved (`承認`), commented on (`コメント："..."`), or left blank (unconfirmed). Only if explicitly requested, `prop-review revise` uses fx to revise commented items from the latest reviewed proposal, preserves pending items, and creates a *new* proposal without automatically starting review. Alternatively discuss or manually draft revised items in a new file. Never change the reviewed original or overwrite its result.

Approval applies only to the specific item. An empty answer is not approval. Never start implementation, convert to zintent approval, or mutate Spec Kit artifacts solely because an item says `承認`; wait for an explicit human request. If `prop-review` is unavailable, say so and provide the two-line template for manual review instead.
