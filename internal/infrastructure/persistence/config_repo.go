package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainconfig "neo-shadaloo/internal/domain/config"
)

type configRepository struct {
	pool *pgxpool.Pool
}

func NewConfigRepository(pool *pgxpool.Pool) domainconfig.ConfigRepository {
	return &configRepository{pool: pool}
}

func (r *configRepository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key,
	).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (r *configRepository) Set(ctx context.Context, key, value string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO app_config (key, value, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET
			value      = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
	`, key, value, time.Now().UnixMilli())
	return err
}
