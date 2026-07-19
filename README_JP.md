# Agent Usage

[![CI](https://github.com/senna-lang/herdr-agent-usage/actions/workflows/ci.yml/badge.svg)](https://github.com/senna-lang/herdr-agent-usage/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)
![herdr 0.7+](https://img.shields.io/badge/herdr-0.7%2B-6E56CF)
![platforms: linux | macOS](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-lightgrey)

[English](README.md) | **日本語**

[Herdr](https://herdr.dev) 上で動くエージェントのコンテキスト使用量と、プロバイダーのレート制限を監視します。

![プロバイダー制限とペインごとの活動シェアを表示する Agent Usage ペイン](docs/assets/agent-usage-pane.png)

- **ペインごとのコンテキストメーター** — 対応エージェントのサイドバーラベルに、セッションがコンテキストウィンドウの何割を使っているかを表示します（`⛁ 13% (130k)` = 130k トークン、ウィンドウの 13%）。各ターン完了後に更新されます。Codex / Grok のラベルは最も制約の厳しいアカウント枠、Antigravity/`agy` は 5 時間枠を表示します。
- **アカウントのレート制限ウィンドウを一覧** — 1 つのライブペインで、Codex / Antigravity / Z.ai / Grok の 5h / 7d / 30d 残量、リセットまでのカウントダウン、どのオープンペインが消費しているかを表示します。Claude と OpenCode のコレクターは利用可能ですが、プラグインペインではデフォルトで非表示です。
- **残量低下の警告** — 任意のトースト通知で、閾値を下回ったときに知らせます（デフォルトは残り 50 / 20 / 10 / 5 %）。タスクの途中で壁に当たる前に気づけます。

## 要件

- **Herdr ≥ 0.7.0**
- **macOS または Linux**
- セッション照合を安定させるためのエージェント連携（推奨）:

```bash
herdr integration install codex
herdr integration install opencode
# Claude ペインを使う場合は Claude Code 連携も推奨
```

## インストール

```bash
herdr plugin install senna-lang/herdr-agent-usage
# 非対話シェル（CI、コーディングエージェント）では --yes が必要
```

プラグインのインストールだけでは `~/.config/herdr/config.toml`（トースト配信、キーバインド）は書き換えられません。インストール後にセットアップを実行してください:

```bash
herdr plugin action invoke usagebar.setup
# 任意: 未設定ならトースト配信を追記
herdr plugin action invoke usagebar.enable-toast
herdr server reload-config
```

`usagebar.setup` は初回実行時に `usagebar` バイナリを自動解決します。ローカルに Go ツールチェーン（≥ 1.25）があればビルドし、なければ [GitHub Releases](https://github.com/senna-lang/herdr-agent-usage/releases) からプリビルドバイナリをダウンロードします（macOS / Linux、arm64 / amd64）。手動ビルドする場合は、プラグインルートで `make build` を実行してください。

## LLM にセットアップさせる

[docs/LLM-SETUP.md](docs/LLM-SETUP.md) のプロンプトを LLM コーディングエージェントに貼り付けてください。
エージェントがプラグインをインストールし、残りのセットアップを案内します。

- **トースト:** トースト通知を有効にする前に、エージェントは必ず承認を求めてください。
- **キーバインド:** 推奨ショートカットは、制限ペインを開く `ctrl+shift+u` と、メーターを更新する `ctrl+shift+m` です（単一コード、Herdr プレフィックスなし）。どちらかが既に使われている場合、エージェントは代わりのショートカットを確認してください。

## クイックスタート

1. プラグインをインストールし、上記の **setup** を実行します。
2. エージェントペインを 1 つ以上含むワークスペースを開きます。
3. エージェントのターンが完了する（またはペインにフォーカスする）と、サイドバーのカスタムステータスにコンテキスト使用量が表示されます。対応エージェントのラベルにはアカウント枠も含まれます（例: `codex 7%`、`grok 94%`、`agy 100%`）。
4. 制限ペインを開きます:

```bash
herdr plugin action invoke usagebar.open-limits
```

5. 任意のキーバインドを **自分の** `~/.config/herdr/config.toml` に追加します:

```toml
[[keys.command]]
key = "ctrl+shift+u"
type = "plugin_action"
command = "usagebar.open-limits"
description = "Agent Usage: open limits pane"

[[keys.command]]
key = "ctrl+shift+m"
type = "plugin_action"
command = "usagebar.refresh"
description = "Agent Usage: refresh sidebar meters"
```

Mac では **Control+Shift+U** / **Control+Shift+M** です（Command ではありません）。その後 `herdr server reload-config` を実行します。

## アクション

| アクション | コマンド | 内容 |
| --- | --- | --- |
| 制限ペインを開く | `usagebar.open-limits` | プロバイダーウィンドウ付きの分割ペインを開く |
| メーターを更新 | `usagebar.refresh` | 対象ペインのサイドバーカスタムステータスを再計算 |
| セットアップ | `usagebar.setup` | プラグイン設定を初期化し、トースト/キーのスニペットを表示、Herdr トースト状態を報告 |
| トーストを有効化 | `usagebar.enable-toast` | `[ui.toast]` が無いときだけ追記（上書きしない） |
| 更新を確認 | `usagebar.check-updates` | GitHub Releases を今すぐ確認し、リリース/更新手順を表示 |

```bash
herdr plugin action list --plugin usagebar
herdr plugin action invoke usagebar.setup
```

## 得られるもの

| 表示面 | 内容 |
| --- | --- |
| **サイドバーのエージェントラベル** | アカウント枠: Codex/Grok は最も制約の厳しいウィンドウ、Antigravity/`agy` は 5 時間ウィンドウ |
| **サイドバーのカスタムステータス** | ペインごとのコンテキスト使用量: ウィンドウサイズが分かる場合は `⛁ 13% (130k)`、分からない場合はトークン数のみ |
| **Agent Usage ペイン** | プロバイダープラン、使用ウィンドウ（5h / 7d / 30d）、残量 % バー、リセットカウントダウン、オープンペインのトークンシェア |
| **トースト**（任意） | 設定した閾値での残量警告（デフォルトは残り 50 / 20 / 10 / 5 %） |

### 対応エージェント

| エージェント | サイドバーコンテキスト | 制限ペイン | 備考 |
| --- | --- | --- | --- |
| Claude Code | あり | デフォルト非表示 | レートウィンドウは `~/.claude.json` / statusLine キャッシュ |
| Codex | あり | あり | コンテキストとレートウィンドウはローカルの rollout から |
| Antigravity | ラベルのみ | あり | `agy` ラベルは 5 時間枠。ローカルクォータ API には起動中プロセスと `lsof` が必要 |
| Z.ai | なし | あり | Coding-plan のクォータ API。`Z_AI_API_KEY` を設定 |
| OpenCode Go | あり | デフォルト非表示 | `OPENCODE_GO_COOKIE` がある場合は公式 Web 使用量を優先、なければローカル SQLite |
| Grok | あり | あり | ラベルは最も制約の厳しい枠。コンテキストは `signals.json` から |

制限ペインのパーセンテージは **残量**（`% left`）です。高いほど安全です。

### Z.ai の設定

Herdr を起動する環境に `Z_AI_API_KEY` を設定します。任意設定は CodexBar と同様です:

- `Z_AI_API_REGION=bigmodel-cn` で `open.bigmodel.cn` を選択（デフォルトは `api.z.ai`）。
- `Z_AI_API_HOST` で API ホストを上書き、`Z_AI_QUOTA_URL` でクォータ URL 全体を上書き。認証情報を含む上書きは HTTPS 必須です。
- チーム利用には `Z_AI_USAGE_SCOPE=team`、`Z_AI_BIGMODEL_ORGANIZATION`、`Z_AI_BIGMODEL_PROJECT` が必要です。

Antigravity に外部クレデンシャルは不要です。コレクターは `127.0.0.1` のみをプローブし、ローカルサーバーの自己署名 TLS 証明書を受け入れます。制限を更新する前に、Antigravity アプリ、IDE、または認証済み `agy` プロセスを起動してください。

## Agent Usage ペイン

- **15 秒**ごとに自動更新。**`r`** で手動更新、**`q`** で終了。
- プラグインペインは Claude と OpenCode 以外の対応プロバイダーを表示します。対応する Herdr エージェントペインが開いていなくても表示されます。
- Claude / OpenCode のサポートコードは削除されていません。デフォルト除外は [`bin/run-limits-pane.sh`](bin/run-limits-pane.sh) の起動引数です。
- CLI を直接使う場合は、除外を付けなければ全コレクターを表示できます。非表示にするには `--exclude-provider <id>` を繰り返します:

```bash
bin/usagebar limits --once --all
bin/usagebar limits --once --all \
  --exclude-provider claude \
  --exclude-provider opencode
```

- OpenCode Go は 3 つのウィンドウ（**5h / 7d / 30d**）を表示することがあります。他のプロバイダーは、データソースが提供する使用ウィンドウを表示します。
- オープンペインの **トークンシェア** は、最短ウィンドウ内でのローカル活動シェアです（オープンペイン外の使用を表す **closed / other** バケットを含む）。アカウントクォータの按分ではありません。
- サイドバーのコンテキストメーターは、エージェントが **settled** になった後（`working` 中ではない）に更新されるため、ラベルは最後に完了したターンと一致します。セッションを解決できない場合は、別セッションの数値を出さずカスタムステータスをクリアします。

```bash
herdr plugin action invoke usagebar.open-limits
```

## 設定

### プロバイダーの表示

Herdr プラグインペインは次のデフォルトで起動します:

```bash
usagebar limits --all \
  --exclude-provider claude \
  --exclude-provider opencode
```

`--all` は対応する Herdr エージェントペインが無くてもプロバイダーを収集します。`--exclude-provider` は表示のフィルタのみで、コレクターの削除や無効化はしません。

### プラグイン設定

```bash
herdr plugin config-dir usagebar
# → ~/.config/herdr/plugins/config/usagebar/config.toml
```

初回の `usagebar.setup`（または未作成時）で生成されます:

```toml
[notify]
enabled = true
remaining_thresholds = [50, 20, 10, 5]
```

### Herdr トースト配信

画面上に通知を出すには必要です:

```bash
herdr plugin action invoke usagebar.enable-toast
herdr server reload-config
```

または `~/.config/herdr/config.toml` に手動で貼り付けます:

```toml
[ui.toast]
delivery = "herdr" # または "system" / "terminal"

[ui.toast.herdr]
position = "bottom-left"
```

`usagebar.enable-toast` は **`[ui.toast]` が無いときだけ追記**します。既存のトースト設定は変更しません。

### OpenCode Go の公式使用量（任意）

OpenCode コンソール由来の数値を使いたい場合は、Cookie リクエストヘッダーを設定します:

```bash
export OPENCODE_GO_COOKIE='auth=…'
```

未設定の場合、使用量はローカルの `~/.local/share/opencode/opencode.db` から推定されます（5h ローリング、UTC 週、カレンダー月）。

### Claude statusLine（任意）

Claude の 5h / 7d ウィンドウとトースト用に、status line をこのプラグイン経由にします。既存コマンドを置き換えず、チェーンしてください。

```json
{
  "statusLine": {
    "type": "command",
    "command": "bash /path/to/herdr-agent-usage/bin/run-statusline.sh"
  }
}
```

インストール後、パスは `herdr plugin list` で解決できます（Herdr 設定下のプラグインルート）。`HERDR_PLUGIN_ROOT` が利用可能なら、`usagebar.setup` が貼り付け用コマンドを表示します。

## レート制限アラート

設定した残量レベル（デフォルト **残り 50% / 20% / 10% / 5%**）で、ウィンドウごとに 1 回ずつ発火します。

1. トースト配信を有効化（`usagebar.enable-toast` または手動スニペット）。
2. **Claude** — 上記の statusLine が利用率をキャッシュして通知します。
3. **Codex / OpenCode / Grok** — エージェントターンが settled になった後、Claude status line なしでも最短の利用可能ウィンドウに基づいてトーストを出せます。

## リリース（メンテナー向け）

1. `herdr-plugin.toml` の `version` を更新し、コミットして `main` に push します。
2. クリーンで最新の `main` チェックアウトから `scripts/release.sh vX.Y.Z` を実行します。

スクリプトは、そのコミットの CI 完了を待ってからタグを作成・push します。タグ起動の Release ワークフローは、GitHub Release 作成前に vet / build / test / format / lint / vulnerability チェックを再度実行します。

## データの扱い

コンテキスト使用量は、エージェントが既にマシン上に保持しているファイルから計算します。プロバイダー制限は、認証付き API または loopback のみの API からも取得します:

| プロバイダー | 読み取るローカルソース |
| --- | --- |
| Claude Code | `~/.claude.json`、`~/.claude/herdr-usagebar/` 配下の statusLine キャッシュ |
| Codex | `~/.codex/sessions/` 配下の rollout ファイル |
| Antigravity | 起動中の Antigravity/`agy` プロセスフラグと `127.0.0.1` のクォータ API |
| Z.ai | `Z_AI_API_KEY` と任意の region/team 環境変数。ローカル使用量ファイルは無し |
| OpenCode Go | `~/.local/share/opencode/opencode.db` |
| Grok | `~/.grok/sessions/**/signals.json`、`~/.grok/auth.json`（クレジット取得用クレデンシャル） |

ネットワークリクエストが発生するケース:

- `opencode.ai` — `OPENCODE_GO_COOKIE` を設定した場合のみ
- `grok.com` — `~/.grok/auth.json` がある場合のみ（`grok login` 済み）
- `api.z.ai` または `open.bigmodel.cn` — `Z_AI_API_KEY` を設定した場合のみ。認証情報を含むエンドポイント上書きは HTTPS 必須
- `127.0.0.1` — Antigravity/`agy` のローカルクォータプローブ。自己署名 TLS はこの固定 loopback 対象のみ許可
- `api.github.com` — 初回のペインフォーカス時、およびその後最大 24 時間に 1 回、このプラグインの最新公開リリースを確認。認証情報は付けず、使用量やセッションデータも送りません

テレメトリ、分析、使用量/セッションデータの送信はありません。プラグインが書き込む状態（設定、通知状態、更新確認状態、使用履歴）は `~/.config/herdr/plugins/config/usagebar/` と `~/.claude/herdr-usagebar/` にのみ置かれます。

## 制限事項

- **課金ダッシュボードではありません。** ローカルの transcript / rollout / signals とプロバイダーのクォータ API は、公式コンソールと一致しないことがあります（他マシン、サーバー側ウィンドウ、API 変更など）。
- **インストール時に Herdr 本体の設定は書き換えません。** `usagebar.setup` / `usagebar.enable-toast` を使うか、手で編集してください。
- **macOS / Linux** のみ。

## ライセンス

[MIT](LICENSE)
