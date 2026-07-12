# gh-skill-tui

[English](README.md) | **日本語**

**gh-skill** + **TUI** = **gh-skill-tui**

## What is gh-skill-tui?

`gh skill` は agent skills を GitHub リポジトリからインストールや管理を行える、非常に便利な CLI です。`gh skill` は CLI なので、複数の agent skills や複数の agent を同時に管理するのは複雑になります。`gh-skill-tui` は TUI 上で複数の agent skills と複数の agent を同時に管理できます。TUI なので視覚的にもわかりやすく、操作も簡単です。

## Who is this for?

- agent skills を外部の skill を使わずに private のリポジトリで管理したい場合。
- agent skills を TUI でインストールや管理をしたい場合。

## Installation

ビルドには Go 1.22 以上が必要です。また gh コマンドを使いますので、先にインストールしてください。
[GitHub CLI](https://cli.github.com/)

```sh
# Install gh-skill-tui / gh-skill-check
git clone https://github.com/Kololu777/gh-skill-tui.git
cd gh-skill-tui
go install .
ln -s "$(go env GOPATH)/bin/gh-skill-tui" "$(go env GOPATH)/bin/gh-skill-check"
```

`go install` は `$(go env GOPATH)/bin`（通常 `~/go/bin`）にバイナリを置きます。ここを PATH に追加しておくと、`gh-skill-tui` / `gh-skill-check` をそのまま実行できます。`gh-skill-check` は同じバイナリへの symlink で、コマンド名によって動作が切り替わります（`gh-skill-tui check` でも同じです）。

インストール後は `gh-skill-tui --version` でバージョンを確認できます。Nix ビルドはパッケージのバージョン、ソースビルドはビルド時の git リビジョンを表示します。

または

```sh
nix build --impure --expr \
  '(builtins.getFlake "nixpkgs").legacyPackages.${builtins.currentSystem}.callPackage ./package.nix {}'
./result/bin/gh-skill-tui OWNER/skills-repo
./result/bin/gh-skill-check
```

`home-manager` では次のように追加できます。

```nix
home.packages = [ (pkgs.callPackage ./package.nix { }) ];
```

## Usage

### 起動方法

```sh
# config.toml に source を指定して起動する場合
gh-skill-tui

gh-skill-tui OWNER/skills-repo
```

source は必須です。CLI 引数・設定ファイルの `source`・環境変数 `GH_SKILL_DEFAULT_SOURCE` のいずれでも指定されていない場合はエラーで終了します。

install / update / delete / PR の計画を作るには、パネル `0:tree`、`1:skills`、`2:agents` で対象の skills と agents を選択し、実行したいアクションキー（`i`（install / update）/ `d`（delete）/ `p`（PR））を押すと、パネル `4:preview` に確認画面が表示されます。確認画面の内容を確認して、`enter` で実行します。

### 画面について

| パネル        | 役割                                                            |
| ------------- | --------------------------------------------------------------- |
| `0 tree`      | source の skill と source 外の skill の tree 構造で表示されます |
| `1 skills`    | source の skill と source 外の skill の一覧が表示されます       |
| `2 agents`    | インストール先のエージェントを選択できます                      |
| `3 installed` | エージェントごとのインストール済み数を確認                      |
| `4 preview`   | skill の内容、インストール先、計画、実行結果を確認              |

#### 画面に表示されるマークについて

**選択状態**

- `[ ]` / `[x]` — 操作対象として選択しているか。`[x]` が選択中
- `[~]` — tree パネルで、配下の一部だけが選択中

**source とローカルとの状態**

- `✓` — 設定した source からインストール済みで最新
- `↓` — インストール後に source が更新された
- `m` — インストール先のコピーを手元で編集した
- `O` — 設定した source の管理外にあるコピー（手動配置、別 source など）

#### key map

| キー                 | 動作                                                                  |
| -------------------- | --------------------------------------------------------------------- |
| `0`–`4`              | パネルを切り替える。`4` はプレビューへ移動                            |
| `h` / `l`            | 前後のパネルへ移動                                                    |
| `j` / `k`, `g` / `G` | カーソルを移動、先頭 / 末尾へ移動                                     |
| `space`              | 現在の skill または agent を選択 / 解除。tree では配下をまとめて選択  |
| `i`                  | Install / Update / Adopt の計画を作る                                 |
| `p`                  | 編集済み skill や outside skill を source へ戻す PR / MR の計画を作る |
| `d`                  | 管理対象のコピーを削除する計画を作る                                  |
| `u`                  | `project` と `user` を切り替える                                      |
| `f`                  | force を切り替える。agents パネルでは現在の destination だけに適用    |
| `r`                  | インストール状況を再スキャンする                                      |
| `s` / `/`            | skill を名前やパスで検索する                                          |
| `enter`              | 通常画面では詳細を表示、計画画面では実行、結果画面では TUI に戻る     |
| `q` / `esc`          | 戻る、検索を解除する、または終了する                                  |

プレビューが長いときは `ctrl+d` / `ctrl+u` で半ページスクロールできます。

### project scope と user scope について

- **project scope** — 現在のディレクトリから親方向へ探した Git リポジトリのルートを基準に配置します（Git リポジトリがなければ現在のディレクトリ）。プロジェクトごとに使う skill に向いています
- **user scope** — ホームディレクトリ配下へ配置します。どのプロジェクトでも使う skill に向いています

## Configuration

設定ファイルの場所は OS ごとに異なります（Go の [`os.UserConfigDir()`](https://pkg.go.dev/os#UserConfigDir) に従います）。

| OS      | パス                                                     |
| ------- | -------------------------------------------------------- |
| Linux   | `~/.config/gh-skill-tui/config.toml`                     |
| macOS   | `~/Library/Application Support/gh-skill-tui/config.toml` |
| Windows | `%AppData%\gh-skill-tui\config.toml`                     |

また、起動するローカルディレクトリにも `.gh-skill-tui.toml` を置くことができます。

```toml
source = "OWNER/private-skills" # GitHub リポジトリの source

# Optional: branch or hash
branch = "main"                         # ref でもよい
hash = "16c1dceffe9c1a80ef615d4347f065ffb71b3101"

scope = "project"               # select: [project / user]
default_agents = ["claude-code", "codex", "opencode"] # 起動時に選択

# Optional: diff command
diff_command = "delta --color-only --paging=never"

# Optional: PR / MR body template (path; relative paths resolve from the project root)
pr_template = ".github/skill_pr_template.md"

# Optional: check_ignore_skills (used by the `gh-skill-check` command)
check_ignore_skills = ["local-only", "tools/*"]
```

### PR / MR のテンプレート (`pr_template`)

`p` で作る PR / MR の本文をテンプレートファイルから組み立てられます。`pr_template` にファイルパス（Markdown、YAML などのテキストファイル）を指定してください。相対パスは project root 基準、`~` も使えます。

テンプレート内では次のプレースホルダーが使えます。

- `{{title}}` — 自動生成される PR タイトル
- `{{body}}` — 自動生成される本文（source skill、コピー元、tree hash などの詳細）

`{{body}}` を書かなかった場合は、テンプレートの末尾に自動生成の詳細が追記されます。

```markdown
## Summary

{{title}}

## Details

{{body}}

## Checklist

- [ ] SKILL.md をレビューした
```

## 対応しているエージェントと配置先

| agent            | user scope                     | project scope      |
| ---------------- | ------------------------------ | ------------------ |
| `claude-code`    | `~/.claude/skills`             | `.claude/skills`   |
| `codex`          | `~/.codex/skills`              | `.agents/skills`   |
| `opencode`       | `~/.config/opencode/skills`    | `.opencode/skills` |
| `kimi`           | `~/.agents/skills`             | `.agents/skills`   |
| `github-copilot` | `~/.copilot/skills`            | `.agents/skills`   |
| `cursor`         | `~/.cursor/skills`             | `.agents/skills`   |
| `gemini`         | `~/.gemini/skills`             | `.agents/skills`   |
| `antigravity`    | `~/.gemini/antigravity/skills` | `.agents/skills`   |

## skill の check (`gh-skill-check`)

設定したい source の skill が現在の project scope にある skill と一致しているかを確認できます。CI や pre-commit で skill が最新であるかを確認するコマンドとして使えます。

```sh
gh-skill-check
gh-skill-check --scope project
gh-skill-check --scope user
```

特定の skill を check の対象から外したい場合は、`.gh-skill-tui.toml` に `check_ignore_skills` を設定します。パターンに一致した skill は、source より古い場合・手元で編集済みの場合・source 管理外の場合のいずれも無視されます。

```toml
check_ignore_skills = ["local-only", "tools/*"]
```

## Release Notes

変更履歴は [release-notes.md](release-notes.md) を参照してください。

## License

MIT License
