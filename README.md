# prop-review

提案を項目ごとに人間が承認・コメントする、ローカルの小さなCLIです。zintentの意味確認・承認ゲートとは別物です。

## インストール（全リポジトリで利用）

このリポジトリのルートで実行します。`~/.local/bin` を `PATH` に含めてください。

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.pi/agent/skills"
GOBIN="$HOME/.local/bin" go install .
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.pi/agent/skills/$skill" ] && [ ! -L "$HOME/.pi/agent/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.pi/agent/skills/$skill"
  fi
done
```

Pi 以外で Agent Skills のユーザー共通ディレクトリ `~/.agents/skills` に対応するツールを使う場合は、こちらにもリンクします。

```sh
mkdir -p "$HOME/.agents/skills"
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.agents/skills/$skill" ] && [ ! -L "$HOME/.agents/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.agents/skills/$skill"
  fi
done
```

既に Skill の配置先がある場合は上書きしません。リンク先を確認してください。Pi を起動中なら `/reload` で Skill を再読み込みします。ツール固有の Skill ディレクトリを使う場合は、そのディレクトリに同様にリンクしてください。

```sh
prop-review
```

リポジトリ内で `prop-review` を実行すると、リポジトリルートの `prop-review-tmp/review.*/proposal.txt` から最新の未レビュー提案を開きます。見つからなければエラーになります。パスを指定する従来の `prop-review proposal.txt` も使えます。新しい提案を作っても以前の `review.*` と結果は削除・上書きしません。CLI は必要に応じてリポジトリのローカル `info/exclude` に `/prop-review-tmp/` を追記します（追跡ファイルは変更しません）。

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

## 提案生成・改訂（fx / Pi）

`fx` または `pi` CLI をインストール・認証し、使用する CLI 側でモデルを設定してから、リポジトリ内の端末で実行します。既定は fx です。libfx やモデル API は使いません。

```sh
prop-review generate                # fx で提案 → 自動レビュー
prop-review generate --engine pi    # Pi で提案 → 自動レビュー
prop-review revise --engine pi      # Pi でコメントを改訂 → 新規 proposal.txt のみ保存
prop-review revise                 # fx でコメントを改訂
prop-review                        # 改訂案を再レビュー
```

両 CLI にはリポジトリの読み取りと提案だけを指示し、実装・ファイル変更は指示しません。Pi は一時セッションの print モードで起動し、拡張・Skill を無効にして読み取り専用ツールだけを渡します。返答は JSON の提案 1〜10 件として検証し（内容に応じて10件程度、無理に水増ししません）、従来の空欄付き `proposal.txt` に変換します。改訂はコメントのある項目だけを置き換え、未確認項目は維持し、承認済み項目は元の提案・結果に残します。改訂後にレビューは自動で開きません。選択した CLI が失敗・不正な出力を返した場合や入力をキャンセルした場合、レビューは開始せず、以前のファイルは維持します。標準の `proposal.review.txt` がないレビュー（保存先を明示したレビュー）は `revise` の対象外です。

チャットでレビュー結果を読み込むときは `prop-review-ingest` Skill を使います。生成・レビューへの案内は `prop-review` Skill が担当し、結果の取り込みは行いません。承認はその項目への意思表示だけです。実装や作業の開始は自動化しません。
