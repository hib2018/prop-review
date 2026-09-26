---
name: zcomment
description: Turn a human's rough idea ("こういうのを考えてる") into a short list of individually reviewable proposals, then collect per-item approval or comments with zcomment. Use when the human asks to start a lightweight proposal/comment session. Not for zintent approval, Spec Kit definitions, or automatic implementation.
---

# zcomment: idea to proposal

Start from the human's own words, in their language. This is a lightweight discussion, not an approval gate for a whole project.

1. If the idea lacks the minimum context to propose anything concrete, ask one short question. Otherwise write 1–5 independently reviewable items. Make assumptions explicit in the item; do not invent requirements or silently expand scope.
2. Create a temporary directory with `mktemp -d` and write `proposal.txt` there in exactly this format (blank approval lines, no other headings):

   ```text
   何について：<one concrete proposal>
   承認/コメント：

   何について：<another proposal>
   承認/コメント：
   ```

3. Show the items to the human and give the exact command `zcomment <absolute-path-to-proposal.txt>`. The CLI needs the human's terminal; do not run it from a non-interactive agent shell or answer on the human's behalf. Pause here until the human says they have finished reviewing or supplies the result.
4. Read `<absolute-path-to-proposal.review.txt>` after the human confirms completion. Report which items were approved (`承認`), commented on, or left blank (unconfirmed). For comments, discuss or draft revised items in a *new* proposal file; do not change the reviewed original or overwrite its result.

Approval applies only to the specific item. An empty answer is not approval. Never start implementation, convert to zintent approval, or mutate Spec Kit artifacts solely because an item says `承認`; wait for an explicit human request. If `zcomment` is unavailable, say so and provide the two-line template for manual review instead.
