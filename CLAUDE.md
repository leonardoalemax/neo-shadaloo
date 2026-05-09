# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o neo-shadaloo .

# Run (requires .env)
./neo-shadaloo

# Run without building
go run .

# Regenerate Swagger docs (run after changing handler annotations or domain types)
swag init --generalInfo main.go --dir . --output docs --parseDependency --parseInternal

# Tidy dependencies
go mod tidy

# Build Docker image
docker compose up --build
```

## Architecture

Go HTTP server that acts as a caching proxy between the Street Fighter 6 Buckler API and a PostgreSQL database (Supabase). The Next.js frontend (`cfnview`) fetches data from this server instead of hitting SF6 directly.

### Request flow

```
Next.js Server Component
  → GET /v1/battlelog/{userId}          ← always served from Postgres
      └─ if cache stale (>5 min): fire-and-forget goroutine
             → SF6 Buckler API (paginated JSON)
             → merge with existing DB data
             → save to Postgres
             → broadcast via WebSocket hub

Browser (overlay page)
  → WS /v1/battlelog/{userId}/ws        ← receives {type:"update", cachedAt} on sync
  → GET /v1/battlelog/{userId}          ← refetch after update message
```

### Key design decisions

- **Serve first, sync later**: `GET /v1/battlelog/{userId}` always returns cached Postgres data immediately — it never blocks on the SF6 API. The sync runs in a background goroutine via `sync.Syncer`.
- **SF6 build ID**: The SF6 API URL contains a Next.js build ID (`FxMUIoPtSKOc3agoNJLwS`) that changes on every SF6 deploy. It is stored in the `app_config` table (`key = "sf6_build_id"`) and auto-refreshed from the SF6 HTML when a 404 is received.
- **One sync goroutine per user**: `Syncer.running` map prevents duplicate syncs for the same userId.
- **WebSocket hub**: `ws.Hub` maps `userId → []conn`. After a successful sync, the syncer calls `ws.Default.Broadcast(userId, ...)` to push `{type:"update"}` to all connected overlay clients.

### Packages

| Package | Role |
|---|---|
| `internal/types` | Shared SF6 JSON types (no dependencies on other internal packages) |
| `internal/db` | Postgres pool (`pgxpool`) + `GetCached`/`SaveCached` for `user_battlelog` table |
| `internal/config` | Read/write `app_config` table (key-value store) |
| `internal/sf6` | HTTP client for SF6 Buckler API; build ID resolution with DB cache + auto-refresh |
| `internal/sync` | Background sync orchestration; merges SF6 data with DB cache |
| `internal/ws` | WebSocket hub; upgrades connections, manages per-user client sets, broadcasts updates |
| `internal/handlers` | chi HTTP handlers wiring the above packages |

### Database tables

```sql
-- Cached replay data per player
user_battlelog (user_id TEXT PK, replays JSONB, banner_info JSONB, cached_at BIGINT)

-- Key-value config (currently only sf6_build_id)
app_config (key TEXT PK, value TEXT, updated_at BIGINT)
```

### Environment variables

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string |
| `SF6_COOKIE` | Full cookie header string from an authenticated SF6 Buckler session |
| `PORT` | HTTP listen port (default `8080`) |

`.env` is loaded automatically via `godotenv` at startup (ignored if absent, e.g. in Docker where vars come from the compose file).
