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
	if err := migrate(ctx, pool); err != nil {
		return err
	}
	return seedCountries(ctx, pool)
}

// seedCountries popula a tabela home_country com o mapping SF6 → ISO 3166-1.
// É idempotente (ON CONFLICT DO NOTHING).
func seedCountries(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, c := range SF6Countries {
		if _, err := tx.Exec(ctx, `
			INSERT INTO home_country (home_id, name, iso3)
			VALUES ($1, $2, $3)
			ON CONFLICT (home_id) DO UPDATE SET
				name = EXCLUDED.name,
				iso3 = EXCLUDED.iso3
		`, c.HomeID, c.Name, c.ISO3); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
		// Mapping home_id (SF6) → país + ISO 3166-1 alpha-3
		`CREATE TABLE IF NOT EXISTS home_country (
			home_id INT PRIMARY KEY,
			name    TEXT NOT NULL,
			iso3    TEXT NOT NULL
		)`,
		// league_player: tabela dedicada do ranking de league.
		// PK = short_id; players são upserted, nunca deletados.
		`CREATE TABLE IF NOT EXISTS league_player (
			short_id            BIGINT PRIMARY KEY,
			fighter_id          TEXT   NOT NULL,
			character_id        INT    NOT NULL DEFAULT 0,
			character_tool_name TEXT   NOT NULL DEFAULT '',
			character_name      TEXT   NOT NULL DEFAULT '',
			league_point        INT    NOT NULL DEFAULT 0,
			league_rank         INT    NOT NULL DEFAULT 0,
			master_league       INT    NOT NULL DEFAULT 0,
			master_rating       INT    NOT NULL DEFAULT 0,
			home_id             INT    NOT NULL DEFAULT 0,
			platform_id         INT    NOT NULL DEFAULT 0,
			order_no            INT    NOT NULL DEFAULT 0,
			full_data           JSONB,
			updated_at          BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS league_player_home  ON league_player (home_id) WHERE home_id > 0`,
		`CREATE INDEX IF NOT EXISTS league_player_order ON league_player (order_no)`,
		// league_meta: singleton (id=1) com estado do sync.
		`CREATE TABLE IF NOT EXISTS league_meta (
			id               INT    PRIMARY KEY DEFAULT 1,
			total_count      INT    NOT NULL DEFAULT 0,
			total_pages      INT    NOT NULL DEFAULT 0,
			synced_pages     INT    NOT NULL DEFAULT 0,
			updated_at       BIGINT NOT NULL DEFAULT 0,
			started_at       BIGINT NOT NULL DEFAULT 0,
			last_synced_at   BIGINT NOT NULL DEFAULT 0,
			status           TEXT   NOT NULL DEFAULT '',
			CONSTRAINT league_meta_singleton CHECK (id = 1)
		)`,
		`INSERT INTO league_meta (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
	}

	for _, s := range steps {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
