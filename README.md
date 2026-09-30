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

リポジトリ内で `prop-review` を実行すると、リポジトリルートの `prop-review-tmp/review.*/proposal.txt` から最新の未レビュー提案を開きます。見つからなければエラーになります。パスを指定する従来の `prop-review proposal.txt` も使えます。新しい提案を作っても以前の `review.*` ディレクトリと結果は残します。不要な履歴は作業終了後に手動で削除してください。

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

通常のレビュー操作は結果を記録します。fxによる改訂案の生成は、以下の `revise` コマンドで明示的に開始します。

## fx CLIで改訂案を生成する

導入・ログイン済みの [Vercel Labs fx](https://github.com/vercel-labs/fx) を別プロセスとして呼び出します。初案の生成は従来どおりSkillを使うエージェントが担当します。

```sh
prop-review                    # 人間が初案をレビューする
prop-review revise             # 最新のレビュー結果から改訂案を生成する
prop-review                    # 人間が改訂案をレビューする
```

`revise`はコメントのある項目だけをfxに改訂させます。未確認項目は原文のまま新しい提案に残し、承認済み項目は元の提案・レビュー結果に記録として残します。改訂案の承認欄はすべて空欄です。項目の承認はコード実装の開始を意味しません。

改訂案は新しい `prop-review-tmp/review.*/proposal.txt` に保存します。同じディレクトリの `source.txt` に元のレビュー結果のパスを記録し、元ファイルは変更しません。`prop-review-tmp/` はGitのローカル除外に追加します。生成された案のパスだけを標準出力へ出すため、そのパスを保存して直接レビューすることもできます。

```sh
proposal=$(prop-review revise)
prop-review "$proposal"
```

fx側のモデル設定を既定で使います。モデルを指定する場合は `--model` を付けます。認証と利用料金はfx側で選んだ接続に従います。

```sh
prop-review revise --model openai/gpt-6.1-sol
prop-review revise --model openai/gpt-6.1-sol /absolute/path/proposal.review.txt
```

明示する結果ファイルは `.review.txt` で終わる名前にし、対応する元の `.txt` ファイルを同じ場所に残してください。カスタム保存先を使った場合もこの組を用意してください。コマンドは保存先となるリポジトリの中から実行します。パスを省略した場合、最新の提案が未レビューなら古いレビューを流用せずエラーにします。コメントのないレビューも改訂対象にしません。

fxには提案とレビュー結果だけを標準入力で渡し、空の一時ディレクトリで `fx ask --json --no-save` を実行します。ツールを使わず提案だけを返すよう指示し、`FX_PERMISSION_MODE=ask` を設定します。これはOSのサンドボックスではありません。プロジェクトのコード探索や実装はこのコマンドの担当にしません。

10分でタイムアウトし、Ctrl+Cでも中断できます。fxが失敗した場合や出力が不正な場合、新しい提案は保存されません。実行ファイルを変更するには `PROP_REVIEW_FX_BIN=/absolute/path/to/fx` を指定します。
