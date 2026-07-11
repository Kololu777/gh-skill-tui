# gh-skill-tui

複数の AI エージェントへ [Agent Skills](https://agentskills.io/) をまとめてインストール・更新するための TUI（ターミナル画面のアプリ）です。

GitHub CLI の `gh skill install` を内部で呼び出し、次の作業を1画面で行えます。

- 使いたい skill を選ぶ
- Claude Code / Codex など、インストール先のエージェントを選ぶ
- インストール済みか、更新があるかを確認する
- 手元で編集した skill を source リポジトリへの PR / MR として提案する
- 管理対象の skill を安全に削除する

このツールは AI エージェント本体をインストールするものではありません。エージェントが読む skill ファイルを、適切なディレクトリへ配置します。

画面仕様の詳しい案: [simplified-ui-proposal.html](./simplified-ui-proposal.html)

## まずは5分で使う

### 1. 必要なもの

- [GitHub CLI (`gh`)](https://cli.github.com/) — `gh skill install` が使える preview 対応版
- `git` — ローカルの skill リポジトリや PR / MR 機能を使う場合に必要
- Go 1.22 以上 — ソースからビルドする場合に必要

GitHub の非公開リポジトリを使う場合や PR を作る場合は、先にログインします。

```sh
gh auth login
gh skill install --help
```

### 2. ビルドする

```sh
git clone https://github.com/Kololu777/gh-skill-tui.git
cd gh-skill-tui
go build -o gh-skill-tui .
```

この README の例では、ビルドしたディレクトリから `./gh-skill-tui` を実行します。`PATH` の通った場所へインストールしたい場合は、代わりに次を実行してください。

```sh
go install .
gh-skill-tui --help
```

`go install` 後に `gh-skill-tui` が見つからない場合は、`go env GOPATH` で表示される `bin` ディレクトリを `PATH` に追加してください。

### 3. skill の source を指定して起動する

source とは、skill が入っている GitHub リポジトリです。リポジトリ内に `SKILL.md` を含むディレクトリが1つ以上必要です。

```sh
./gh-skill-tui OWNER/skills-repo
```

たとえば、このプロジェクトに組み込みの既定値を使う場合は引数なしでも起動できます。

```sh
./gh-skill-tui
```

既定の source は `Kololu777/private-skills` です。自分のリポジトリを使うときは、必ず `OWNER/REPO` を指定するか、設定ファイルで変更してください。

### 4. 画面でインストールする

起動後は、次の順番で操作します。

1. `1` で **skills パネル**へ移動し、`j` / `k` で skill を選び、`space` でマークする
2. `2` で **agents パネル**へ移動し、選択状態を確認する。追加したい agent は `space` で選び、不要な agent は `space` で解除する
3. 必要なら `u` で `project` / `user` のインストール範囲を切り替える
4. `i` を押してインストール計画を開く
5. 表示された計画を確認し、問題なければ `enter`（または `y`）で実行する

通常は `claude-code`、`codex`、`opencode`、`kimi` が最初から選択されています。不要なエージェントは agents パネルで `space` を押して外してください。

`i` を押しただけではインストールされません。必ず計画画面で確認してから `enter` を押す設計です。実行後は結果を表示し、インストール状況を自動的に再スキャンします。

## 用語を先に理解する

| 用語 | 意味 |
|---|---|
| skill | エージェントに追加する手順書。通常は skill ディレクトリ内の `SKILL.md` |
| source | skill の原本が入っている GitHub リポジトリ、またはローカル git clone |
| agent | skill を利用する AI エージェント。例: Codex、Claude Code |
| destination | skill のインストール先ディレクトリ |
| scope | インストール範囲。`project` は現在のプロジェクト、`user` は自分のホームディレクトリ |

たとえば Codex の project scope なら `.agents/skills` に、user scope なら `~/.codex/skills` に配置されます。

## 画面の見方

| パネル | 役割 |
|---|---|
| `0 tree` | ディレクトリごとに skill を絞り込む。`space` で配下をまとめて選択 |
| `1 skills` | source の skill と、source 外に存在する skill を選択 |
| `2 agents` | インストール先のエージェントを選択 |
| `3 installed` | エージェントごとのインストール済み数を確認 |
| `4 preview` | skill の内容、インストール先、計画、実行結果を確認 |

選択状態とインストール状態は別です。

- `[ ]` / `[x]` — 操作対象として選択しているか。`[x]` が選択中
- `[~]` — tree パネルで、配下の一部だけが選択中
- `✓` — 設定した source からインストール済みで最新
- `↓` — インストール後に source が更新された
- `m` — インストール先のコピーを手元で編集した
- `O` — 設定した source の管理外にあるコピー（手動配置、別 source など）

`m` や `O` は角括弧の外側に表示されます。これは「選択中」という意味ではありません。

## よく使うキー

| キー | 動作 |
|---|---|
| `0`–`4` | パネルを切り替える。`4` はプレビューへ移動 |
| `h` / `l` | 前後のパネルへ移動 |
| `j` / `k`, `g` / `G` | カーソルを移動、先頭 / 末尾へ移動 |
| `space` | 現在の skill または agent を選択 / 解除。tree では配下をまとめて選択 |
| `i` | Install / Update / Adopt の計画を作る |
| `p` | 編集済み skill や outside skill を source へ戻す PR / MR の計画を作る |
| `d` | 管理対象のコピーを削除する計画を作る |
| `u` | `project` と `user` を切り替える |
| `f` | force を切り替える。agents パネルでは現在の destination だけに適用 |
| `r` | インストール状況を再スキャンする |
| `s` / `/` | skill を名前やパスで検索する |
| `enter` / `y` | 通常画面では詳細を表示、計画画面では実行、結果画面では TUI に戻る |
| `q` / `esc` | 戻る、検索を解除する、または終了する |

プレビューが長いときは `ctrl+d` / `ctrl+u` で半ページスクロールできます。

## project scope と user scope

起動時の既定値は `project` です。

- **project scope** — 現在のディレクトリから親方向へ探した git リポジトリのルートを基準に配置します（git リポジトリがなければ現在のディレクトリ）。プロジェクトごとに使う skill に向いています
- **user scope** — ホームディレクトリ配下へ配置します。どのプロジェクトでも使う skill に向いています

操作中は `u` で切り替えられます。起動時から指定する場合は次のようにします。

```sh
./gh-skill-tui --scope user OWNER/skills-repo
```

## 対応しているエージェントと配置先

| agent | user scope | project scope |
|---|---|---|
| `claude-code` | `~/.claude/skills` | `.claude/skills` |
| `codex` | `~/.codex/skills` | `.agents/skills` |
| `opencode` | `~/.config/opencode/skills` | `.opencode/skills` |
| `kimi` | `~/.agents/skills` | `.agents/skills` |
| `github-copilot` | `~/.copilot/skills` | `.agents/skills` |
| `cursor` | `~/.cursor/skills` | `.agents/skills` |
| `gemini` | `~/.gemini/skills` | `.agents/skills` |
| `antigravity` | `~/.gemini/antigravity/skills` | `.agents/skills` |

project scope では複数のエージェントが `.agents/skills` を共有します。同じ destination になる場合は、同じ skill を重複してインストールしません。

表にないエージェントも、設定ファイルの `[[agents]]` で追加できます。

## source の指定方法

### GitHub リポジトリ

```sh
./gh-skill-tui OWNER/skills-repo
./gh-skill-tui --source OWNER/skills-repo --ref main
```

`--ref` を省略すると、リポジトリの既定ブランチを使います。

### GitLab などのローカル git clone

GitHub 以外のリポジトリは、ローカルへ clone してディレクトリを指定します。

```sh
git clone git@gitlab.com:OWNER/private-skills.git ~/src/private-skills
./gh-skill-tui ~/src/private-skills
```

ローカル source には次の制約があります。

- git リポジトリである必要があります
- `git ls-files` に出てくる追跡済みファイルだけが対象です
- 更新検知の基準は clone の `HEAD` です。先に `git pull` してください
- インストール時には `--from-local` を使い、TUI が追跡用メタデータを追加します

## Install / Update / Delete の安全な動き

### インストールと更新

`i` は、選択した skill × agent の組み合わせを計画に変換します。最新のコピーはスキップし、更新があるものは `UPDATE` として表示します。

計画に1件でも解決できない対象があると、計画全体が `BLOCKED` になります。`Ready` と表示された対象も含め、`enter` を押すまで何も実行されません。選択を見直すか、プレビューに表示される理由を解消してから再実行してください。

特に次の状態に注意してください。

- `m`（手元で編集済み）は、差分を確認してから `f` で上書きを許可します。force はローカルの編集を失わせる可能性があります
- `O`（source 外）のコピーに対する `i` は実行できません。source へ取り込むには `p` を使います
- `d` は、追跡情報を確認できる managed コピーだけを削除します。`O` のコピーは保護されます

### 手元の編集を PR / MR にする

インストール先で編集した skill に `m` が付いたら、skill を選んで `p` を押します。複数の skill を選んだ場合も、1 branch / 1 commit / 1 PR（または MR）にまとめます。

- GitHub source: `gh` の API で branch と commit を作り、PR を作成します
- ローカル clone: `origin` へ branch を push し、GitLab などの push option に対応したサーバーでは MR 作成も依頼します

source 外の skill（`O`）を取り込みたい場合も `p` を使えます。インストール先の相対ディレクトリをそのまま source の `skills/` 以下へ写像します。たとえば `sakuramoti/foo` は `skills/sakuramoti/foo` になります。親ディレクトリが無い場合も PR のファイルパスから作成されます。

```text
.agents/skills/
`-- sakuramoti/foo/SKILL.md
    |
    +-- p
        v
private-skills/
`-- skills/sakuramoti/foo/SKILL.md
```

PR / MR の作成には、source リポジトリへ push できる権限が必要です。作成前にプレビューで差分を確認してください。

## 非対話の監査 (`gst check`)

TUI を開かず、現在の project scope にある skill を CI や pre-commit から監査できます。`gst` はパッケージが提供する `gh-skill-tui` の短縮名です（`gh-skill-tui check` も同じ動作です）。

```sh
gst check
```

監査では次を確認します。

- 設定した source の managed skill が、指定した branch / hash の最新版であること。古い場合は終了コード `1` になり、`gh skill update --all` または再インストールの案内を表示します
- 現在の project 側の skill directory に、source 外の skill がないこと。見つかった場合は終了コード `1` になり、`gh-skill-tui` の `p` で private-skills へ追加する PR を提案できます

監査は読み取り専用です。自動 update、push、PR 作成は行いません。

## コマンドラインオプション

```text
gh-skill-tui [--source OWNER/REPO|DIR] [--ref REF] [--scope project|user] [--agent AGENT]
             [--dir DIR] [--force] [--config PATH] [--select] [--dry-run]
             [OWNER/REPO|DIR] [gh skill install flags...]
```

| オプション | 用途 |
|---|---|
| `OWNER/REPO` または `DIR` | source を指定する。`--source` の代わりに位置引数でも指定可能 |
| `--source SOURCE` | GitHub リポジトリまたはローカル git clone を指定する |
| `--ref REF` | GitHub source の branch / tag などを指定する |
| `--scope project\|user` | 起動時の scope を指定する。既定は `project` |
| `--agent AGENT` | 指定した `gh skill` の agent だけを対象にする |
| `--dir DIR` | すべての選択 skill を指定ディレクトリへ配置する |
| `--force` / `-f` | 既存コピーを上書きする。手元の編集も失われる可能性がある |
| `--config PATH` | 設定ファイルを変更する |
| `--select` | TUI で選んだ source skill のパスだけを標準出力へ出して終了する |
| `--dry-run` | install / update / delete / PR を実行せず、実行予定だけを表示する |
| その他の `gh skill install` フラグ | `--pin` などをそのまま渡す |

`--agent` または `--dir` を指定した場合、agents パネルの選択は無視されます。

例:

```sh
# Codex の user scope だけを対象にする
./gh-skill-tui --agent codex --scope user OWNER/skills-repo

# 指定ディレクトリへ入れる計画を確認する
./gh-skill-tui --dir ~/.config/my-agent/skills --dry-run OWNER/skills-repo

# スクリプトから選択された skill のパスを受け取る
./gh-skill-tui --select OWNER/skills-repo
```

## 設定

設定の優先順位は次の通りです。

```text
CLI フラグ > 環境変数 > 設定ファイル > 組み込み既定値
```

設定ファイルは通常、`~/.config/gh-skill-tui/config.toml` です。OS によって設定ディレクトリが異なる場合は `--config PATH` で明示できます。ファイルがなくても動作します。

現在の directory から git repository root まで、`.gh-skill-tui.toml` を探します。`gh-skill-tui.toml`、`.gst.toml`、`gst.toml` も互換名として使えます。project-local 設定はユーザー設定より優先され、CLI フラグと環境変数には負けます。

```toml
# .gh-skill-tui.toml
source = "Kololu777/private-skills"
branch = "main"                         # ref でもよい
hash = "16c1dceffe9c1a80ef615d4347f065ffb71b3101"

# project 側だけにある skill を意図的に許可する場合
check_ignore_skills = ["local-only", "tools/*"]
```

`hash` は `gh skill install --pin` に渡されるため、この project だけ特定 revision に固定できます。`commit` / `pin` は `hash` の別名、`ref` は `branch` の別名です。ignore は未追跡・source 外の判定にだけ適用され、private-skills 管理下の古い skill は引き続きエラーになります。`[check]` テーブルの `ignore_skills` または `check-ignore-skill` も利用できます。

最初は source と agent だけ設定すれば十分です。

```toml
source = "OWNER/my-skills"
scope = "user"
default_agents = ["codex", "claude-code"]
```

利用できる設定項目の例:

```toml
source = "OWNER/private-skills"       # 既定 source
scope = "project"                     # project / user
force = false                         # 既定で force を有効にするか
default_agents = ["claude-code", "codex"]
allowed_sources = ["OWNER/private-skills"]
allowed_local_roots = ["~/src/private-skills"]

# diff の色付けを外部コマンドに任せる。未設定なら内蔵表示を使う
diff_command = "delta --color-only --paging=never"

# agent = "" なら gh の --dir 方式で配置する
[[agents]]
name = "my-agent"
short = "mine"
agent = ""
user_dir = "~/.config/my-agent/skills"
project_dir = ".my-agent/skills"
```

環境変数でも次の値を上書きできます。

| 環境変数 | 内容 |
|---|---|
| `GH_SKILL_DEFAULT_SOURCE` | 既定 source |
| `GH_SKILL_DEFAULT_AGENTS` | 起動時に選択する agent（カンマ区切り） |
| `GH_SKILL_DEFAULT_SCOPE` | 既定 scope（`project` / `user`） |
| `GH_SKILL_ALLOWED_SOURCES` | 許可する GitHub source（カンマ区切り） |
| `GH_SKILL_ALLOWED_LOCAL_ROOTS` | managed として扱うローカル clone の root（カンマ区切り） |

## 更新検知と source 外のコピー

GitHub source では skill ディレクトリの git tree SHA、ローカル source では clone の `HEAD` にある tree SHA を比較します。skill を変更していない commit だけでは `↓` になりません。

インストール先のファイルも source と比較しているため、手元の編集は `m` として検知されます。`SKILL.md` に `gh` や TUI が追加する追跡メタデータは比較時に正規化されるので、インストール処理そのものを編集と誤認しません。

`O` は次のような source 外のコピーをまとめた表示です。

- 手動配置・自作など、追跡メタデータがない
- 別のリポジトリからインストールした
- 以前は source にあったが、現在の source には対応するパスがない

TUI はすべての対応 destination をスキャンし、managed と outside を分けて表示します。source 外のコピーを勝手に update / delete することはありません。

## Nix / home-manager でインストールする

Nix を使う場合は、このリポジトリの `package.nix` を利用できます。

```sh
nix build --impure --expr \
  '(builtins.getFlake "nixpkgs").legacyPackages.${builtins.currentSystem}.callPackage ./package.nix {}'
./result/bin/gh-skill-tui OWNER/skills-repo
```

home-manager では次のように追加できます。

```nix
home.packages = [ (pkgs.callPackage ./package.nix { }) ];
```

## 開発

```sh
go vet ./... && go test ./...
```

主な実装は次のファイルに分かれています。

- `tui.go` — 画面とキー操作
- `targets.go` — agent と destination の対応、インストール計画
- `status.go` — インストール状況のスキャンと分類
- `skills.go` — GitHub / git からの source 読み込み
- `pr.go` — PR / MR と diff
- `config.go` — 設定ファイル

## 困ったときは

- `gh skill` が見つからない: `gh skill install --help` が通る GitHub CLI を用意してください
- source を読めない: `gh auth status` を確認し、非公開リポジトリなら `gh auth login` を実行してください
- skill が一覧に出ない: 対象ディレクトリに `SKILL.md` があるか確認してください。ローカル source ではファイルが git に追跡されている必要があります
- `BLOCKED` になる: preview パネルの理由を確認してください。`m` は差分確認後に `f`、`O` は `p` で source への PR / MR を作るのが基本です

## 制限事項

- `gh skill` は preview 機能のため、GitHub CLI 側の仕様変更の影響を受ける可能性があります
- ローカル source の更新検知は clone の `HEAD` が基準です。TUI は自動で `fetch` / `pull` しません
- symlink の skill ディレクトリもスキャンしますが、`.` で始まるベンダー領域（例: `~/.codex/skills/.system`）は対象外です
