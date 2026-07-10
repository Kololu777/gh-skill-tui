# gh-skill-tui

[Agent Skills](https://agentskills.io/) を複数の AI エージェント CLI へまとめてインストール・管理するための lazygit 風 TUI です。`gh skill install` のラッパーとして動作し、skill の選択から対象 agent の選択、導入状況の監査、更新検知、手元で編集した skill の PR 化までを 1 画面で行えます。

ブラウザで見る最終仕様: [simplified-ui-proposal.html](./simplified-ui-proposal.html)

```
gh-skill-tui  source: you/private-skills  ref: main  dir: ~/proj   scope:project
┌─ 0 tree ──────────────────┐┌─ 4 INSTALL / UPDATE PLAN — BLOCKED ───┐
│ [~] (all) (13)            ││ Ready — 2 install commands:            │
│ [~] skills/ (12)          ││   UPDATE  share/memoc-write → codex    │
│ [x] (outside source) (1)  ││   INSTALL tools/gpu-setup → codex      │
├─ 1 skills (12+1 outside) ─┤│                                         │
│ [ ]✓ share/memoc-create 1/1││ Blocking reasons:                      │
│ [x]↓ share/memoc-write  1/1││   ✗ outside source                     │
│ [x]  tools/gpu-setup    0/1││                                         │
│ [x]O my-local              ││       • my-local                        │
├─ 2 agents ────────────────┤│ Nothing will run. Esc returns.          │
│ [x]✓ claude-code          ││                                         │
│ [x]↓ codex update         ││                                         │
├─ 3 installed ─────────────┤│                                         │
│ p codex managed:2 outside:1││                                        │
└───────────────────────────┘└─────────────────────────────────────────┘
 scope:project  selected: source 2 / outside 1  |  i install  p PR  d delete
```

## 特徴

- **複数 agent への一括インストール** — Claude Code / Codex / OpenCode / Kimi Code / GitHub Copilot / Cursor / Gemini CLI / Antigravity に対応(設定ファイルで任意の agent を追加可能)。同一ディレクトリに解決される agent は自動で重複排除
- **導入状況の監査** — 設定ソースで管理中のコピーと、設定ソース外のコピー(`O`)を分離。外部 repo / 追跡なし / ソース path 消失の理由は詳細画面で確認可能
- **更新検知** — git の tree SHA を突き合わせ、「インストール後にソース側が更新された」コピーを `↓` で表示
- **手元編集と outside skill の PR 化** — `m` の編集差分と `O` の追加を、選択した分すべて 1 branch / 1 commit / 1 PR にまとめて提案
- **原子的な安全チェック** — 選択中に操作不能な対象が 1 件でもあれば plan 全体を `BLOCKED` にし、Ready 部分も含めて何も実行しない
- **共通実行フロー** — Install/Update/Adopt、PR、Delete はすべて plan → terminal log → result → rescan の同じ流れ
- **GitHub でも GitLab でも** — GitHub リポジトリは gh 経由、GitLab などは git clone をローカルソースとして利用(`--from-local`)

## 必要なもの

- [GitHub CLI (`gh`)](https://cli.github.com/) — `gh skill` コマンドが使えるバージョン(preview 機能)
- `git` — ローカルソース・diff・PR 機能で使用
- ビルドには Go 1.22+(または Nix)

## インストール

```sh
# Go
go build -o gh-skill-tui .

# Nix (このリポジトリの package.nix)
nix build --impure --expr \
  '(builtins.getFlake "nixpkgs").legacyPackages.${builtins.currentSystem}.callPackage ./package.nix {}'
```

home-manager なら:

```nix
home.packages = [ (pkgs.callPackage ./package.nix { }) ];
```

## 使い方

```
gh-skill-tui [--source OWNER/REPO|DIR] [--ref REF] [--scope project|user] [--agent AGENT]
             [--dir DIR] [--force] [--config PATH] [--select] [--dry-run]
             [OWNER/REPO|DIR] [gh skill install flags...]
```

```sh
gh-skill-tui                          # 既定ソース(設定ファイル/環境変数で指定)を開く
gh-skill-tui owner/skills-repo        # GitHub リポジトリを開く
gh-skill-tui ~/src/private-skills     # ローカル git clone を開く(GitLab など)
gh-skill-tui --scope user             # user scope 起動(~/.claude/skills など)
gh-skill-tui --dry-run                # 実行せずコマンドを表示
gh-skill-tui --select                 # 選んだ skill のパスを表示するだけ(スクリプト用)
```

基本フロー: **skill を `space` で選択 → agent を panel 2 で選択 → `i` / `p` / `d` で操作を選ぶ → plan を確認して `enter`**。角括弧は全パネルで選択だけを表し、`[n]` / `[u]` / `[d]` のような操作状態は持ちません。

plan は選択全体を検証します。たとえば source skill と outside skill を同時に選んで `i` を押すと、実行可能な install/update は `Ready` に表示されますが、outside skill が `Blocking` に入るため **enter は無効で、Ready も実行されません**。対象を外すか、outside skill を `p` で source へ提案してからやり直します。

BLOCKED の診断は理由別にまとめます。同じ「project scope に編集済みコピーがない」が 8 skill で発生した場合、理由を 8 回繰り返さず、理由の下に 8 skill を一覧します。

実行時は一時的に通常の terminal へ切り替わって実ログを流し、完了後に panel 4 の結果画面へ自動復帰して再スキャンします。失敗した対象の選択は再試行用に残ります。

### パネル

| # | パネル | 内容 |
|---|--------|------|
| 0 | tree | ディレクトリツリー。選択で panel 1 を絞り込み。`space` で配下を一括マーク |
| 1 | skills | source skill と outside skill の一覧。`[ ]` / `[x]` は中立選択、直後の記号は状態 |
| 2 | agents | Install/Update/Delete の対象 destination。outside 行ではコピー所在の監査表示のみ |
| 3 | installed | agent ディレクトリごとの managed / outside 集計(u=user / p=project) |
| 4 | preview | 詳細、SKILL.md、diff、操作 plan、実行結果 |

### キーバインド

| キー | 動作 |
|------|------|
| `0`–`4` / `h` / `l` | パネル移動(4 = プレビューにフォーカスしてスクロール) |
| `j` / `k`, `g` / `G` | カーソル移動 / 先頭・末尾 |
| `space` | 中立選択をトグル。tree は配下一括、agents は destination 選択。outside 行の agent panel では無効 |
| `i` | 選択を Install / Update / Adopt として解決し、plan を開く |
| `p` | 選択した `m` と `O` を 1 件の PR/MR plan にまとめる |
| `P` | outside の現在行だけ、source 側の追加先を手動選択。それ以外は `p` と同じ |
| `d` | 選択した managed コピーだけを Delete plan にする。outside は blocker |
| `enter` / `y` | normal screen: panel 4 の詳細を開く。plan: 実行。result: TUI に戻る |
| `u` | scope 切替(project ⇔ user) |
| `f` | agent panel: その destination の明示的 overwrite/reinstall override。それ以外: 全 agent の force 切替 |
| `s` / `/` | インクリメンタル検索 |
| `r` | 導入状況を再スキャン |
| `ctrl+d` / `ctrl+u` | プレビューを半ページスクロール |
| `q` / `esc` | 戻る / 検索クリア / 終了 |

### 状態記号

角括弧は常に `[ ]` 未選択 / `[x]` 選択です。tree の `[~]` だけは配下の一部選択を表します。角括弧の直後に状態を表示します:

| 記号 | 意味 |
|------|------|
| `✓` (緑) | 許可ソースからインストール済み・最新 |
| `↓` (橙) | インストール済みだがソース側に新しい内容がある |
| `m` (紫) | インストール済みだが**手元のコピーが編集されている**(`p` で PR 化可能) |
| `O` (黄) | 設定 source の管理外。外部 repo / no-track / source path 消失を一本化した表示 |
| 空白 | 選択 agent に managed コピーがない |

source skill 行の末尾には、同じ物理 destination を重複排除した導入範囲 `N/M` を表示します。agent 名の右にある `new` / `update` / `overwrite` / `reinstall` は plan の予告であり、角括弧の意味を変えません。

## Agent 対応表

`gh skill install` がネイティブ対応していない agent には `--dir` で配置します。

| agent | 方式 | user scope | project scope |
|----------|------|------------|---------------|
| claude-code | `--agent` | `~/.claude/skills` | `.claude/skills` |
| codex | `--agent` | `~/.codex/skills` | `.agents/skills` |
| opencode | `--dir` | `~/.config/opencode/skills` | `.opencode/skills` |
| kimi | `--dir` | `~/.agents/skills` | `.agents/skills` |
| github-copilot | `--agent` | `~/.copilot/skills` | `.agents/skills` |
| cursor | `--agent` | `~/.cursor/skills` | `.agents/skills` |
| gemini | `--agent` | `~/.gemini/skills` | `.agents/skills` |
| antigravity | `--agent` | `~/.gemini/antigravity/skills` | `.agents/skills` |

project scope では多くの agent が `.agents/skills` を共有するため、複数選択しても 1 回だけインストールされます(確認画面に skip 理由を表示)。

表にない agent は設定ファイルの `[[agents]]` で追加できます(同名を定義すると built-in を上書き)。

## 更新検知のしくみ

git の **tree SHA**(ディレクトリ内容のハッシュ)を比較します。

1. インストール時に「入れた時点の SHA」を記録 — GitHub は gh 自身が `github-tree-sha:` を SKILL.md frontmatter に書き込み、ローカルソースでは本ツールが `tui-tree-sha:` を追記
2. 起動時に「ソースの現在の SHA」を取得 — GitHub は tree API、ローカルは `git ls-tree HEAD`
3. 不一致なら `↓`、一致なら `✓`。内容ベースの比較なので、skill に触れていない commit が積まれても誤検知しません

手元編集(`m`)は、コピーの各ファイルの blob SHA をソースと比較して検知します。SKILL.md は gh が注入するメタデータの除去に加え、frontmatter のキー順や空行の違いに影響されない正規化比較を行うため、gh のインストール処理による書き換えを「編集」と誤検知しません。

## GitLab などをソースにする(ローカル git clone)

`gh` は GitHub 専用のため、GitLab のリポジトリは clone をローカルソースとして指定します:

```sh
git clone git@gitlab.com:you/private-skills.git ~/src/private-skills
gh-skill-tui ~/src/private-skills
```

- **git 管理必須**: nix flake と同様、git リポジトリ外のディレクトリはエラー、`git ls-files` で**追跡済みファイルのみ**を列挙します
- 更新検知は **clone の HEAD 基準**です。`git pull` していない分は検知しません(そこは利用者の責任という設計)
- header の ref に `local@<short-sha>` を表示します

## PR / MR 提案(`p` / `P` キー)

インストール先で手直しした managed skill(`m`)と、設定 source 外の skill(`O`)を同時に選択できます。`p` はすべての変更を **1 branch / 1 commit / 1 PR** にまとめます。未編集の source skill など PR にできない選択が混じると plan 全体が BLOCKED になり、部分 PR は作りません。

- **GitHub**: git data API で branch + commit を作成(clone 不要)→ `gh pr create` で PR 作成
- **GitLab clone**: git plumbing で working tree に触れずに branch を作成 → `git push -o merge_request.create`(push options で MR まで作成、`glab` 不要)
- **TUI に戻ってくる**: plan で enter を押すと通常の terminal に切り替わって実行ログを表示し、完了時は自動的に panel 4 の result へ戻ります
- コミット内容は注入メタデータを剥がした正規化版。確認画面で diff を表示
- ソース側が先に進んでいる場合は PR 本文に ⚠ 警告を自動で記載します。**マージ判断はリポジトリ側(=レビューする人)の責任**という割り切りです

### outside source skill の追加 PR

設定 source で管理されていないコピーは panel 1 に `O` 付きで表示され、tree には仮想ノード `(outside source)` が現れます。`O` は次の 3 種類をまとめた表示です:

- 追跡メタデータのない手動配置 / 自作コピー(`no tracking`)
- 別リポジトリから導入したコピー(`external source`)
- 設定 source の追跡情報を持つが、対応 path が現在の source にないコピー(`source path missing`)

表示は現在の scope に従い、`u` で project / user を切り替えます。outside 行の panel 2 は destination 選択ではなくコピー所在の inventory です。コピーがある agent に角括弧外の `O` を表示し、`space` / `f` / `d` はそこでは作用しません。

**`p`(推奨)**: 追加先を ①追跡 path → ②既存 namespace → ③ `new_skill_dir` → ④ source 内の `share` の順で推定します。複数の outside 行と複数の `m` 行を混ぜても 1 PR です。追加先の衝突・曖昧さ・重複 path があれば全体を BLOCKED にします。

**`P`**: outside の現在行だけ追加先を手動指定する 2 段階ピッカーです。既存親 directory または直接入力を選び、次に skill 名を決めます。

PR が merge されて source を再スキャンすると、対応する source 行から `i` を実行できます。同名の outside コピーが destination にあれば plan は `ADOPT` と明示し、`--force` で source 版へ置き換えて追跡情報付き managed(`✓`)にします。outside 行そのものに対する `i` と `d` は常に BLOCKED です。

## サプライチェーン方針

- ソースは allowlist(`GH_SKILL_ALLOWED_SOURCES`)と照合し、外れている場合は header に赤字で `[UNAPPROVED SOURCE]` を表示
- allowlist に含まれる別 repository でも、現在開いている authoritative source と一致しなければ `O external source`。同じ `github-path` だけで managed とみなしません
- panel 3 で全 agent directory を監査し、managed / outside の件数と outside の具体的な理由・元 repository を確認可能
- Delete は追跡メタデータと destination root を検証できる managed directory だけを対象にし、outside コピーには触れません

## 設定

設定は **CLI フラグ > 環境変数 > 設定ファイル > 組み込み既定** の優先順位で解決されます。

### 設定ファイル

`~/.config/gh-skill-tui/config.toml`(`--config PATH` で変更可)。すべて省略可能です:

```toml
source = "you/private-skills"          # 既定ソース
scope  = "user"                        # 既定 scope (project / user)
force  = false                         # 既定で --force を付ける
default_agents = ["claude-code", "codex", "opencode", "kimi"]  # 起動時にマークする agent
allowed_sources = ["you/private-skills"]        # 許可する OWNER/REPO(サプライチェーン方針)
allowed_local_roots = ["~/src/private-skills"]  # managed 扱いにするローカル clone root

# diff を外部コマンドで色付け(ANSI 出力をそのまま表示)
diff_command = "delta --color-only --paging=never"

# p の outside-source 追加 PR で送り先を推定できないときの既定ディレクトリ
# (未設定ならソース内の share/ ディレクトリに置く)
new_skill_dir = "skills/share"

# agent の追加(同名なら built-in を上書き)。agent = "" なら --dir 方式
[[agents]]
name        = "my-agent"
short       = "mine"
agent       = ""
user_dir    = "~/.config/my-agent/skills"
project_dir = ".my-agent/skills"
```

旧設定の `[[providers]]` も互換 alias として読み込みます。新規設定では `[[agents]]` を使用してください。両方に同名定義がある場合は `[[agents]]` が優先されます。

ファイルが無ければ既定値で動作します。壊れた TOML は起動時エラーになります。

### 環境変数(設定ファイルより優先)

| 変数 | 意味 | 既定値 |
|------|------|--------|
| `GH_SKILL_DEFAULT_SOURCE` | 既定のソースリポジトリ | (設定ファイル → 組み込み既定) |
| `GH_SKILL_DEFAULT_AGENTS` | 起動時にマークされる agent(カンマ区切り) | `claude-code,codex,opencode,kimi` |
| `GH_SKILL_DEFAULT_SCOPE` | 既定 scope(`project` / `user`) | `project` |
| `GH_SKILL_ALLOWED_SOURCES` | 許可する OWNER/REPO(カンマ区切り) | 既定ソースと同じ |
| `GH_SKILL_ALLOWED_LOCAL_ROOTS` | managed 扱いにするローカル clone root(カンマ区切り) | (セッションのローカルソースは常に許可) |

## 開発

```sh
go vet ./... && go test ./...
```

機能別にファイルを分けています: `tui.go`(画面・キー処理)/ `targets.go`(agent と destination の対応・インストールプラン)/ `status.go`(導入スキャン・分類・正規化)/ `skills.go`(ソース読み込み・gh/git 実行)/ `pr.go`(PR 作成・diff)/ `config.go`(設定ファイル)/ `tree.go` / `util.go`。

## 制限事項

- `gh skill` は preview 機能のため、gh 側の仕様変更に影響を受ける可能性があります
- 導入スキャンは symlink の skill ディレクトリ(home-manager 管理の nix store リンクなど)も追跡します。一方 `.` 始まりのディレクトリ(codex がビルトイン skill を置く `~/.codex/skills/.system` など)はベンダー領域としてスキャン対象外です
- outside (`O`) は直接 Update/Delete しません。PR merge 後に対応 source 行から ADOPT すると managed になります
- ローカルソースの更新検知は clone の HEAD が基準です(fetch はしません)
