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
	}

	for _, s := range steps {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
