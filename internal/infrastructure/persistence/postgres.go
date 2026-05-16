package persistence

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	cfg.MaxConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}

	if err := pool.Ping(ctx); err != nil {
		return err
	}

	Pool = pool
	return migrate(ctx, pool)
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	steps := []string{
		`CREATE TABLE IF NOT EXISTS char_usage (
			yyyymm    TEXT PRIMARY KEY,
			entries   JSONB  NOT NULL DEFAULT '[]',
			cached_at BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS char_fighting (
			yyyymm    TEXT PRIMARY KEY,
			entries   JSONB  NOT NULL DEFAULT '[]',
			cached_at BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS player_index (
			fighter_id          TEXT PRIMARY KEY,
			short_id            BIGINT  NOT NULL DEFAULT 0,
			character_tool_name TEXT    NOT NULL DEFAULT '',
			updated_at          BIGINT  NOT NULL DEFAULT 0
		)`,
		// Add syncable column to existing tables (no-op if already present)
		`ALTER TABLE player_index ADD COLUMN IF NOT EXISTS syncable BOOLEAN NOT NULL DEFAULT false`,
		`CREATE INDEX IF NOT EXISTS player_index_updated_at ON player_index (updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS player_index_syncable   ON player_index (syncable) WHERE syncable = true`,
		// Ranking global (snapshot atual, ~10M entries por tipo, 4 tipos = ~40M linhas)
		`CREATE TABLE IF NOT EXISTS ranking_entry (
			ranking_type          TEXT   NOT NULL,
			order_no              INT    NOT NULL,
			short_id              BIGINT NOT NULL DEFAULT 0,
			fighter_id            TEXT   NOT NULL DEFAULT '',
			character_id          INT    NOT NULL DEFAULT 0,
			character_tool_name   TEXT   NOT NULL DEFAULT '',
			character_name        TEXT   NOT NULL DEFAULT '',
			league_point          INT    NOT NULL DEFAULT 0,
			league_rank           INT    NOT NULL DEFAULT 0,
			master_league         INT    NOT NULL DEFAULT 0,
			master_rating         INT    NOT NULL DEFAULT 0,
			master_rating_order   INT    NOT NULL DEFAULT 0,
			home_id               INT    NOT NULL DEFAULT 0,
			platform_id           INT    NOT NULL DEFAULT 0,
			full_data             JSONB  NOT NULL DEFAULT '{}',
			PRIMARY KEY (ranking_type, order_no)
		)`,
		`CREATE INDEX IF NOT EXISTS ranking_entry_short_id      ON ranking_entry (short_id)`,
		`CREATE INDEX IF NOT EXISTS ranking_entry_type_char     ON ranking_entry (ranking_type, character_tool_name, order_no)`,
		`CREATE INDEX IF NOT EXISTS ranking_entry_type_home     ON ranking_entry (ranking_type, home_id, order_no)`,
		`CREATE TABLE IF NOT EXISTS ranking_meta (
			ranking_type     TEXT PRIMARY KEY,
			total_count      INT    NOT NULL DEFAULT 0,
			total_pages      INT    NOT NULL DEFAULT 0,
			synced_pages     INT    NOT NULL DEFAULT 0,
			updated_at       BIGINT NOT NULL DEFAULT 0,
			started_at       BIGINT NOT NULL DEFAULT 0,
			last_synced_at   BIGINT NOT NULL DEFAULT 0,
			status           TEXT   NOT NULL DEFAULT ''
		)`,
		// idempotent: adiciona as novas colunas se a tabela já existir sem elas
		`ALTER TABLE ranking_meta ADD COLUMN IF NOT EXISTS started_at     BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE ranking_meta ADD COLUMN IF NOT EXISTS last_synced_at BIGINT NOT NULL DEFAULT 0`,
	}

	for _, s := range steps {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
