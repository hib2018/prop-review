# prop-review

提案を項目ごとに人間が承認・コメントする、ローカルの小さなCLIです。zintentの意味確認・承認ゲートとは別物です。

## インストール（全リポジトリで利用）

このリポジトリのルートで実行します。`~/.local/bin` を `PATH` に含めてください。

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.pi/agent/skills"
GOBIN="$HOME/.local/bin" go install .
if [ ! -e "$HOME/.pi/agent/skills/prop-review" ] && [ ! -L "$HOME/.pi/agent/skills/prop-review" ]; then
  ln -s "$PWD/skills/prop-review" "$HOME/.pi/agent/skills/prop-review"
fi
```

Pi 以外で Agent Skills のユーザー共通ディレクトリ `~/.agents/skills` に対応するツールを使う場合は、こちらにもリンクします。

```sh
mkdir -p "$HOME/.agents/skills"
if [ ! -e "$HOME/.agents/skills/prop-review" ] && [ ! -L "$HOME/.agents/skills/prop-review" ]; then
  ln -s "$PWD/skills/prop-review" "$HOME/.agents/skills/prop-review"
fi
```

既に Skill の配置先がある場合は上書きしません。リンク先を確認してください。Pi を起動中なら `/reload` で Skill を再読み込みします。ツール固有の Skill ディレクトリを使う場合は、そのディレクトリに同様にリンクしてください。

```sh
prop-review
```

リポジトリ内で `prop-review` を実行すると、リポジトリルートの `prop-review-tmp/review.*/proposal.txt` から最新の未レビュー提案を開きます。見つからなければエラーになります。パスを指定する従来の `prop-review proposal.txt` も使えます。Skill が新しい提案を作る際、同フォルダ内の以前の `review.*` ディレクトリ（結果を含む）は削除されます。必要な結果は事前に別の場所へ保存してください。

`proposal.txt` は次の形式で作ります。承認欄は必ず空欄にしてください。

```text
設定を変更する
承認/コメント：

既存データは変更しない
承認/コメント：
```

以前の `何について：` 付きの提案ファイルも読み込めます。結果にはこの接頭辞を出力しません。承認欄のコロンは半角・全角を受け付け、結果には全角で統一します。文字化けした入力（不正なUTF-8や `�`）は受け付けません。

端末で各項目に `a`（即承認）、`c`（コメント入力・Enterで確定）、Enter（未確認）、`b`（前の項目に戻る）、`q`（保存せず中断）を押します。操作キーは全角英字でも使えます。コメントの日本語入力・削除はCLI側で表示を更新します。最初から空行を入力した場合は「未確認扱い」の警告が出ます（Enterで確定、Escで項目に戻る）。

結果は `proposal.review.txt` に保存され、パスが標準出力に表示されます。既存の結果ファイルは上書きしません。作業中に残したい場所を指定するには `prop-review proposal.txt /path/to/result.txt` と実行してください。入力が途中で終了した場合も、結果は保存されません。結果のコメントは `コメント："..."` として承認から区別します。

このツールは結果を記録するだけです。コメントへの対応や作業の開始は自動化しません。
