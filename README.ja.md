# prop-review

[English README](README.md)

提案を項目ごとにレビューするローカルCLIです。ソースコードは `src/` にあります。

## インストール

このリポジトリのルートで実行し、`~/.local/bin` を `PATH` に追加してください。

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.pi/agent/skills"
go build -o "$HOME/.local/bin/prop-review" ./src
# 短縮コマンド（既存の prev は上書きしない）
if ! command -v prev >/dev/null 2>&1 && [ ! -e "$HOME/.local/bin/prev" ] && [ ! -L "$HOME/.local/bin/prev" ]; then
  ln -s prop-review "$HOME/.local/bin/prev"
fi
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.pi/agent/skills/$skill" ] && [ ! -L "$HOME/.pi/agent/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.pi/agent/skills/$skill"
  fi
done
```

Pi以外で共有の `~/.agents/skills` を使う場合は、そこにもリンクできます。

```sh
mkdir -p "$HOME/.agents/skills"
for skill in prop-review prop-review-ingest; do
  if [ ! -e "$HOME/.agents/skills/$skill" ] && [ ! -L "$HOME/.agents/skills/$skill" ]; then
    ln -s "$PWD/skills/$skill" "$HOME/.agents/skills/$skill"
  fi
done
```

既存のリンクは上書きしません。Piを起動済みなら `/reload` します。CLIをソースから更新した場合は `go build` を再実行してください。

## コマンド

Gitリポジトリ内で実行します。画面上ではリポジトリ内のパスを短く表示し、`prop-review-tmp/` 以下は共通部分を省いて `review.xxx/proposal.txt` のように表示します。実際のファイルは `prop-review-tmp/` 以下にあり、コマンドへパスを直接渡す際にはその接頭辞が必要です。`prev` は `prop-review` と同じコマンド・引数を受け付ける短縮名です（例: `prev spec`）。`generate`、`issue`、`revise`、`clarify`、`spec`、`ingest` はリポジトリの `prop-review-tmp/` を使用します。

### `prop-review` / `prop-review proposal.txt [result.txt]`

未レビューの提案が1件なら開き、複数なら一覧から選択します。直接ファイルを指定することもできます。例:

```sh
prop-review
prop-review prop-review-tmp/review.123/proposal.txt
prop-review proposal.txt /path/to/result.txt
```

`a` + Enterで承認、`c` + Enterでコメント、Enterだけで未確認、`b` + Enterで戻り、`q` + Enterで保存せず終了します。全角ラテン文字キーにも対応します。コメントが空の場合は未確認になることを警告し、Enterで確定、Escで戻れます。依頼文・コメント入力は左右キー、Home/End、Backspaceに対応し、コメントの上限は1 MiBです。結果は通常、提案の隣の `proposal.review.txt` に保存し、パスを表示します。既存結果は上書きしません。別の結果パスを指定できますが、その結果は `revise` や `spec` の履歴には使えません。

### `prop-review ingest [NAME]`

```sh
prop-review ingest          # 設定済みエンジンを起動（初期値はpi）
prop-review ingest claude   # 今回だけ変更。fx、codex、agy、opencodeも指定可能
```

このGitリポジトリで対話型エージェントを起動し、`prop-review-ingest` に従って最新の完了済みレビュー（`proposal.txt` の更新時刻で選択）の履歴を検証し、結果の報告とコメントへの回答をチャットで行います。結果がなければエージェントが結果ファイルのパスを尋ねます。CLIとスキルはレビュー結果を変更せず、実装も開始しません。対象のCLIをインストール・認証してください。スキルは `~/.agents/skills/prop-review-ingest/SKILL.md` または `~/.pi/agent/skills/prop-review-ingest/SKILL.md` に配置します（上記のPiセットアップは後者）。他のエージェントにはスキルのパスを明示して渡します。`fx` は対話起動時の初期プロンプト指定に対応しないため、`fx ask --prompt-permissions` で初回応答を保存し、`fx --continue` でそのセッションを開きます。各エージェントの設定・ツール権限が適用され、`ingest` 自体はサンドボックスを設定しません。

### `prop-review generate`

```sh
prop-review generate
```

依頼文を入力して提案を生成し、すぐレビューを開始します。依頼文は最大256 KiBのUTF-8で、前後の空白を除きます。元の依頼はルートの `request.txt` に保存されます。レビューを中断した場合も生成済みの未レビュー提案は残ります。

### `prop-review issue`

```sh
prop-review issue
```

認証済みの `gh` CLIが認識するGitHubリポジトリで、開いているissueを選んで提案を生成・レビューします。候補は `gh issue list` の既定の最初の30件です。タイトル・本文・番号・URLをエンジンへ渡し、タイトルを `request.txt` に保存します。JSONで256 KiBを超えるissueは拒否します。issue自体は変更しません。

### `prop-review revise [proposal.txt]`

```sh
prop-review revise
prop-review revise prop-review-tmp/review.123/proposal.txt
prop-review  # 改訂後の提案を別途レビュー
```

コメント付きのレビュー済み提案を選ぶか、同じリポジトリ内の提案パスを指定します。コメント付き項目だけを改訂し、未確認項目はそのまま引き継ぎ、承認済み項目は以前の履歴に残します。新しい `review.*` ディレクトリに `parent.txt` で履歴をつないで保存します。**改訂後のレビューは自動開始しません。** コメントがなく未確認だけの場合は改訂できません。隣接するレビュー結果がない場合や不一致の場合も失敗します。

### `prop-review clarify`

```sh
prop-review clarify
```

ルートの依頼文・issueタイトルから対象を選び、その系列の承認済み・未確認・コメント付き履歴を検証して追加確認事項を探します。未確認項目と未解決のコメント付き項目を重複なく引き継ぎ（改訂で置き換えられた旧項目や承認済み項目は除外）、合計10件以内で新しい確認事項があれば `followup.txt` と `parent.txt` を持つ提案を作り、レビューを開始します。追加事項も引き継ぐ項目もなければ保存しません。ルートの `request.txt` がない旧レビュー、欠落・不正な結果や曖昧な履歴では失敗します。コメントが残る場合は `revise`、未確認が残る場合は再レビューを推奨します。

### `prop-review spec`

```sh
prop-review spec
```

選択なしで、このリポジトリの**全レビュー系列**の履歴と結果を検証し、承認済み項目の文言だけをエンジンに渡し、読みやすく要約したメモを `prop-review-tmp/spec.md` に保存します。文言のそのままの列挙や出典パスは必須ではありません。要約は誤解や余計な内容を含む可能性があるため、元のレビュー結果と照合してください。追加確認事項や未確認項目があっても確認画面に進み、`prop-review clarify` を推奨します。`request.txt` のない旧レビューは確認できない旨を警告します。追加確認中は系列ごとに表示を増やさず、1つのスピナーを更新します。要約生成中に別のスピナーは表示しません。生成したMarkdown全文と保存先を端末に表示し、新規・上書きともに `y/N` で確認し、`y`（または `Y`）を入力した場合だけ保存します。Enterやそれ以外は中止します。中止時は追加・上書きしません。不正な履歴の系列は丸ごと除外し、正常な系列だけで要約します。除外理由や追加確認できなかった系列は保存前のプレビューに警告として示します。有効な承認項目が残らなければ保存しません。`spec.md` は参照用のメモであり、正式な仕様書や承認記録ではありません。実装や別のレビューは開始しません。

### `prop-review delete`

```sh
prop-review delete
```

未レビューの提案と、コメントがあり子の改訂がないレビュー済み提案から選びます。削除前に提案全文とレビュー結果（承認済み項目を含む）を表示し、`y/N` で確認し、`y`（または `Y`）で確定します。Enterやそれ以外は削除しません。`parent.txt` で参照されるディレクトリ、リンクや想定外のファイルがある候補は拒否します。確定すると選んだ `review.*` ディレクトリを**恒久的に削除**します。

### `prop-review engine` / `prop-review --engine NAME`

```sh
prop-review engine       # 現在のエンジンを表示
prop-review --engine pi  # fx、claude、codex、agy、opencodeも指定可能
```

エンジンはユーザー単位の設定（OSのユーザー設定ディレクトリ内の `prop-review/engine`）で、初期値は `pi` です。変更すると他のリポジトリでも使われます。使用するCLIのインストールと認証が必要です。提案生成でPiを使う際はNode.jsとグローバルnpmパッケージ `@earendil-works/pi-coding-agent` のSDKを使用し、`pi` CLIを直接呼びません。`ingest` は選択したエージェントの対話型CLIを起動します。

## ファイル形式と履歴

`proposal.txt` は項目ごとに「単一行の提案文」と空の `Approval/Comment:` 欄を持ちます。提案の言語は依頼に合わせます。例（組み込みコマンドではありません）:

```text
現在の設定をエクスポートするコマンドを追加する
Approval/Comment:

保存したレビューの復元方法を文書化する
Approval/Comment:
```

結果ファイルの `Approval/Comment:` には空欄（未確認）、`Approved`、Go形式で引用・エスケープされた `Comment: "..."` だけを認めます。提案文は空でない単一行とし、不正なUTF-8、置換文字 `�`、制御文字、同一提案内の同じ文言の重複は拒否します。生成結果・保存時・既存提案の読み取りと履歴検証でも重複を拒否し、既存ファイルを自動修正しません。改訂で同じ項目を引き継ぐ場合など、別世代に同じ文言が残ることはあります。`parent.txt` は直前の `review.*` ディレクトリ名を保持し、`followup.txt` は追加確認の系列を示します。リンクのない旧改訂を同じ系列と推測することはありません。

`generate` と `revise` は固有の `review.*` ディレクトリを作成し、必要ならローカルGitの `info/exclude` に `/prop-review-tmp/` を追加します。レビューだけではGit設定を変更しません。依頼文やコメントがローカルに保存されるため、内容に注意してください。

## エンジンと失敗時の動作

エンジンにはリポジトリの参照と提案の作成（`spec` では承認項目の要約）だけを指示し、実装は指示しません。応答は1～10件の単一行の提案を含むJSON配列として検証し、`clarify` のみ空配列も受け付けます。言語は自動検証しません。処理内容に応じたスピナーを標準エラー出力に表示し、100秒でタイムアウトします。外部CLIの標準出力・Codex結果ファイル・OpenCodeイベント列は各16 MiB、標準エラー出力は64 KiB、`npm root -g` の標準出力は8 KiB、OpenCodeの連結テキストは12 MiBに制限します。超過時は収集中にプロセスを停止し、明示的なエラーを返します。不正な応答・失敗・入力中断時に以前のファイルは上書きしません。

Piは拡張・スキル・プロンプトテンプレートを無効にした読取専用セッション、Claudeは制限モードでRead/Glob/Grepのみ、Codexは読取専用サンドボックス、`agy` はplanモードと端末サンドボックス、OpenCodeはread/glob/grepのみを許可する専用エージェントを使います。`agy` はローカルセッション情報を保存する場合があります。OpenCodeはプロンプトをコマンド引数で渡すため100,000バイトを超えると拒否します。`fx ask --no-save` への指示だけではツール権限を強制できません。外部CLIの設定・フック・プラグインによる副作用には注意してください。

完了したレビュー結果をチャットに取り込み、コメントへの回答を得るには `prop-review-ingest` スキルを使います。`prop-review` スキルは提案生成とレビューへの引き渡しを扱います。承認はその項目だけへの判断であり、実装は自動開始しません。
