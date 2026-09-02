# Fob

A local proxy that turns Claude, Codex, Grok, and Cursor subscriptions into an OpenAI-compatible API. One panel for logins, the keys you hand to other tools, and the usage meter.

Point Cursor, Claude Code, OpenCode, or anything that speaks OpenAI/Anthropic at `http://127.0.0.1:8317/v1`.

## Run (binary)

Download a release for your OS from GitHub Releases, then:

```bash
chmod +x fob
./fob
```

On first boot without `JWT_SECRET`, Fob writes `~/.fob/jwt_secret` (`0600`) and `~/.fob/fob.sqlite`. The seed panel password is printed once. Open http://localhost:8317, change it, login to a provider, mint a `sk-fob-…` key.

## Run (Docker)

```bash
export JWT_SECRET=$(openssl rand -hex 32)
docker compose up --build
```

Compose **requires** `JWT_SECRET`. Data lives on the `fob-data` volume at `/data/fob.sqlite`.

## OAuth

Claude and Codex OAuth apps only allow their CLI callbacks (`http://localhost:54545/callback` and `http://localhost:1455/auth/callback`). While a login is in flight, Fob binds those ports. If the port is already taken (the real CLI), the panel keeps the paste-callback form — paste the whole failed address bar. Grok uses device-code. Cursor uses CLI deep-control poll (Login) or a dashboard API key (Paste key).

## Env

| Var | Default | Role |
|---|---|---|
| `JWT_SECRET` | binary: `~/.fob/jwt_secret` (auto); Docker: required | Encrypts credentials and signs the panel cookie |
| `FOB_HOME` | `~/.fob` | Binary data dir (`fob.sqlite`, `jwt_secret`) |
| `DATABASE_PATH` | binary: `$FOB_HOME/fob.sqlite`; image: `/data/fob.sqlite` | SQLite |
| `HOST` | `0.0.0.0` | Bind |
| `PORT` | `8317` | Listen |
| `LOG_LEVEL` | `info` | |
| `CLAUDE_CLIENT_ID` / `CODEX_CLIENT_ID` / `GROK_CLIENT_ID` | embedded CLI clients | Override |

Cursor effort and fast variants (`-high`, `-medium`, `-fast`, …) collapse to one listed id (`claude-opus-5`, `composer-2.5`). Thinking stays a sibling (`claude-opus-5-thinking`). Pick effort with `reasoning_effort` and fast with `fast: true` or a `-fast` suffix. Ids that already appear on a connected Claude/Codex/Grok catalog are omitted; force Cursor with a `cursor/` prefix (`cursor/claude-opus-5`). Unprefixed `claude-opus-5` still hits Anthropic first and failovers to Cursor on retryable errors. Optional panel toggle maps Grok ↔ Cursor Grok.

## API

- `GET /health`
- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1/responses`

Bearer: a LocalKey (`sk-fob-…`). Meter dollars are **API-equivalent $** from [models.dev](https://models.dev) list prices, not your subscription bill. The panel Sub card is remaining on the subscription (click to load).

## Dev

```bash
make test
go run ./cmd/fob
```
