package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "neo-shadaloo/internal/domain/ranking"
)

type RankingRepository struct {
	pool *pgxpool.Pool
}

func NewRankingRepository(pool *pgxpool.Pool) *RankingRepository {
	return &RankingRepository{pool: pool}
}

// ClearType apaga todas as entries de um ranking_type. Roda antes de iniciar
// um sync novo, pra garantir snapshot atual sem ficar entries velhas.
func (r *RankingRepository) ClearType(ctx context.Context, rt domain.RankingType) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM ranking_entry WHERE ranking_type = $1`, string(rt))
	return err
}

// AppendBatch insere um lote de entries. Usa CopyFrom (binary protocol) que é
// muito mais rápido que INSERT comum pra cargas grandes.
// Em caso de duplicata por (ranking_type, order_no) — que pode acontecer se a
// mesma página for processada duas vezes — usa ON CONFLICT DO UPDATE pra
// reaproveitar; CopyFrom não suporta upsert, então fallback pra INSERT
// quando algo falhar.
func (r *RankingRepository) AppendBatch(ctx context.Context, entries []domain.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	// Tenta CopyFrom primeiro (rápido)
	rows := make([][]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []any{
			string(e.RankingType),
			e.OrderNo,
			e.ShortID,
			e.FighterID,
			e.CharacterID,
			e.CharacterToolName,
			e.CharacterName,
			e.LeaguePoint,
			e.LeagueRank,
			e.MasterLeague,
			e.MasterRating,
			e.MasterRatingOrder,
			e.HomeID,
			e.PlatformID,
			[]byte(e.FullData),
		})
	}

	cols := []string{
		"ranking_type", "order_no", "short_id", "fighter_id",
		"character_id", "character_tool_name", "character_name",
		"league_point", "league_rank", "master_league", "master_rating",
		"master_rating_order", "home_id", "platform_id", "full_data",
	}

	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"ranking_entry"}, cols, pgx.CopyFromRows(rows))
	if err == nil {
		return nil
	}

	// Fallback: se CopyFrom falhar (ex: duplicata), faz INSERT ... ON CONFLICT
	return r.appendBatchUpsert(ctx, entries)
}

func (r *RankingRepository) appendBatchUpsert(ctx context.Context, entries []domain.Entry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const stmt = `
		INSERT INTO ranking_entry (
			ranking_type, order_no, short_id, fighter_id,
			character_id, character_tool_name, character_name,
			league_point, league_rank, master_league, master_rating,
			master_rating_order, home_id, platform_id, full_data
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (ranking_type, order_no) DO UPDATE SET
			short_id            = EXCLUDED.short_id,
			fighter_id          = EXCLUDED.fighter_id,
			character_id        = EXCLUDED.character_id,
			character_tool_name = EXCLUDED.character_tool_name,
			character_name      = EXCLUDED.character_name,
			league_point        = EXCLUDED.league_point,
			league_rank         = EXCLUDED.league_rank,
			master_league       = EXCLUDED.master_league,
			master_rating       = EXCLUDED.master_rating,
			master_rating_order = EXCLUDED.master_rating_order,
			home_id             = EXCLUDED.home_id,
			platform_id         = EXCLUDED.platform_id,
			full_data           = EXCLUDED.full_data
	`

	for _, e := range entries {
		if _, err := tx.Exec(ctx, stmt,
			string(e.RankingType), e.OrderNo, e.ShortID, e.FighterID,
			e.CharacterID, e.CharacterToolName, e.CharacterName,
			e.LeaguePoint, e.LeagueRank, e.MasterLeague, e.MasterRating,
			e.MasterRatingOrder, e.HomeID, e.PlatformID, []byte(e.FullData),
		); err != nil {
			return fmt.Errorf("upsert entry order=%d: %w", e.OrderNo, err)
		}
	}
	return tx.Commit(ctx)
}

// ReplaceAll é uma forma simplificada: clear + appendBatch.
// Útil quando não estamos crawlando incrementalmente.
func (r *RankingRepository) ReplaceAll(ctx context.Context, rt domain.RankingType, entries []domain.Entry) error {
	if err := r.ClearType(ctx, rt); err != nil {
		return err
	}
	return r.AppendBatch(ctx, entries)
}

// SaveMeta grava o meta. last_synced_at só é atualizado se vier > 0 no input
// (assim os updates de progresso não sobrescrevem com 0).
func (r *RankingRepository) SaveMeta(ctx context.Context, meta domain.SnapshotMeta) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ranking_meta (ranking_type, total_count, total_pages, synced_pages, updated_at, started_at, last_synced_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (ranking_type) DO UPDATE SET
			total_count    = EXCLUDED.total_count,
			total_pages    = EXCLUDED.total_pages,
			synced_pages   = EXCLUDED.synced_pages,
			updated_at     = EXCLUDED.updated_at,
			started_at     = CASE WHEN EXCLUDED.started_at > 0     THEN EXCLUDED.started_at     ELSE ranking_meta.started_at     END,
			last_synced_at = CASE WHEN EXCLUDED.last_synced_at > 0 THEN EXCLUDED.last_synced_at ELSE ranking_meta.last_synced_at END,
			status         = EXCLUDED.status
	`,
		string(meta.RankingType), meta.TotalCount, meta.TotalPages, meta.SyncedPages,
		meta.UpdatedAt, meta.StartedAt, meta.LastSyncedAt, meta.Status,
	)
	return err
}

func (r *RankingRepository) GetMeta(ctx context.Context, rt domain.RankingType) (*domain.SnapshotMeta, error) {
	var m domain.SnapshotMeta
	var rtStr string
	err := r.pool.QueryRow(ctx, `
		SELECT ranking_type, total_count, total_pages, synced_pages, updated_at, started_at, last_synced_at, status
		FROM ranking_meta WHERE ranking_type = $1
	`, string(rt)).Scan(
		&rtStr, &m.TotalCount, &m.TotalPages, &m.SyncedPages,
		&m.UpdatedAt, &m.StartedAt, &m.LastSyncedAt, &m.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.RankingType = domain.RankingType(rtStr)
	return &m, nil
}
