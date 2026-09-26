---
name: prop-review
description: Create a proposal.txt of individually reviewable options or next steps from an initial idea, a direction-setting request, or any other topic the human wants to discuss and review item by item; then collect approval or comments with prop-review. Use when the human asks for idea generation, direction or issue analysis with possible next steps, proposals, options, a lightweight review, or invokes prop-review. Not for ordinary information-only answers, zintent approval, Spec Kit definitions, or automatic implementation.
---

# prop-review: request to reviewable proposals

Start from the human's own words, in their language. This is a lightweight discussion, not an approval gate for a whole project. Use it for early ideas and direction-setting, but also for other requests when the human wants to review possible choices or next steps. Do not turn a request for facts only or direct implementation into an approval session without being asked.

1. If the request lacks the minimum context to propose anything concrete, ask one short question. Otherwise write 1–5 small, independently reviewable items. Inspect the relevant project when proposals depend on its actual state. Convert findings into specific proposed responses rather than asking the human to approve a fact; distinguish confirmed problems from optional improvements. Make assumptions explicit in each item; do not invent requirements or silently expand scope.
2. In the repository being discussed (not necessarily the prop-review repository), find its root with `git rev-parse --show-toplevel`. Create `<repo-root>/prop-review-tmp/`, keep it out of Git by adding `/prop-review-tmp/` to that repository's local `info/exclude` (`git rev-parse --git-path info/exclude`) if it is not already ignored, then create a unique directory inside it with `mktemp -d <repo-root>/prop-review-tmp/review.XXXXXX`. Write `proposal.txt` there in exactly this format (blank approval lines, no other headings). If there is no repository, ask where to save instead of falling back to another location:

   ```text
   <one concrete proposal>
   承認/コメント：

   <another proposal>
   承認/コメント：
   ```

3. Show the items to the human and give the exact command `prop-review <absolute-path-to-proposal.txt>`. By default the result is saved beside the proposal as `proposal.review.txt`. These files remain on disk until explicitly deleted; they are not promoted to durable project artifacts automatically. To save the result somewhere durable, the human can specify a destination: `prop-review <absolute-path-to-proposal.txt> <absolute-path-to-result.txt>`; do not silently persist proposals or results elsewhere. The CLI needs the human's terminal; do not run it from a non-interactive agent shell or answer on the human's behalf. Pause here until the human says they have finished reviewing or supplies the result.
4. Read the default `<absolute-path-to-proposal.review.txt>` or the human's chosen result path after the human confirms completion. Report which items were approved (`承認`), commented on (`コメント："..."`), or left blank (unconfirmed). For comments, discuss or draft revised items in a *new* proposal file; do not change the reviewed original or overwrite its result.

Approval applies only to the specific item. An empty answer is not approval. Never start implementation, convert to zintent approval, or mutate Spec Kit artifacts solely because an item says `承認`; wait for an explicit human request. If `prop-review` is unavailable, say so and provide the two-line template for manual review instead.
