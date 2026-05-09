package persistence

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "neo-shadaloo/internal/domain/battlelog"
)

type battlelogRepository struct {
	pool *pgxpool.Pool
}

func NewBattlelogRepository(pool *pgxpool.Pool) domain.BattlelogRepository {
	return &battlelogRepository{pool: pool}
}

func (r *battlelogRepository) GetByUserID(ctx context.Context, userID string) (*domain.Battlelog, error) {
	var replaysRaw, bannerRaw []byte
	var cachedAt int64

	err := r.pool.QueryRow(ctx, `
		SELECT replays, banner_info, cached_at
		FROM user_battlelog
		WHERE user_id = $1
	`, userID).Scan(&replaysRaw, &bannerRaw, &cachedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var replays []domain.Replay
	if err := json.Unmarshal(replaysRaw, &replays); err != nil {
		return nil, err
	}

	var banner *domain.FighterBannerInfo
	if bannerRaw != nil {
		banner = &domain.FighterBannerInfo{}
		if err := json.Unmarshal(bannerRaw, banner); err != nil {
			return nil, err
		}
	}

	return &domain.Battlelog{
		UserID:     userID,
		Replays:    replays,
		BannerInfo: banner,
		CachedAt:   cachedAt,
	}, nil
}

func (r *battlelogRepository) Save(ctx context.Context, b *domain.Battlelog) error {
	replaysJSON, err := json.Marshal(b.Replays)
	if err != nil {
		return err
	}

	var bannerJSON []byte
	if b.BannerInfo != nil {
		bannerJSON, err = json.Marshal(b.BannerInfo)
		if err != nil {
			return err
		}
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO user_battlelog (user_id, replays, banner_info, cached_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET
			replays     = EXCLUDED.replays,
			banner_info = EXCLUDED.banner_info,
			cached_at   = EXCLUDED.cached_at
	`, b.UserID, replaysJSON, bannerJSON, b.CachedAt)
	return err
}
