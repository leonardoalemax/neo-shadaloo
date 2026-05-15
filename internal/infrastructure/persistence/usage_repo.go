package persistence

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "neo-shadaloo/internal/domain/usage"
)

type usageRepository struct {
	pool *pgxpool.Pool
}

func NewUsageRepository(pool *pgxpool.Pool) domain.UsageRepository {
	return &usageRepository{pool: pool}
}

func (r *usageRepository) Get(ctx context.Context, yyyymm string) (*domain.UsageSnapshot, error) {
	var leaguesRaw []byte
	var cachedAt int64

	err := r.pool.QueryRow(ctx, `
		SELECT entries, cached_at
		FROM char_usage
		WHERE yyyymm = $1
	`, yyyymm).Scan(&leaguesRaw, &cachedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var leagues []domain.LeagueUsage
	if err := json.Unmarshal(leaguesRaw, &leagues); err != nil {
		return nil, err
	}

	return &domain.UsageSnapshot{YYYYMM: yyyymm, Leagues: leagues, CachedAt: cachedAt}, nil
}

func (r *usageRepository) ListMonths(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT yyyymm FROM char_usage
		WHERE entries != '[]'::jsonb AND entries != 'null'::jsonb
		ORDER BY yyyymm DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var months []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		months = append(months, m)
	}
	return months, nil
}

func (r *usageRepository) Save(ctx context.Context, s *domain.UsageSnapshot) error {
	leaguesJSON, err := json.Marshal(s.Leagues)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO char_usage (yyyymm, entries, cached_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (yyyymm) DO UPDATE SET
			entries   = EXCLUDED.entries,
			cached_at = EXCLUDED.cached_at
	`, s.YYYYMM, leaguesJSON, s.CachedAt)
	return err
}
