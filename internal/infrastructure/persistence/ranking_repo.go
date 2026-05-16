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

// ── Queries de visualização ──────────────────────────────────────────────────

const maxLimit = 200
const defaultLimit = 50

// List devolve uma página de entries com filtros opcionais.
func (r *RankingRepository) List(ctx context.Context, rt domain.RankingType, f domain.ListFilter) (*domain.ListPage, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	// Constrói WHERE dinamicamente
	where := "ranking_type = $1"
	args := []any{string(rt)}
	idx := 2
	if f.CharacterToolName != "" {
		where += fmt.Sprintf(" AND character_tool_name = $%d", idx)
		args = append(args, f.CharacterToolName)
		idx++
	}
	if f.HomeID != 0 {
		where += fmt.Sprintf(" AND home_id = $%d", idx)
		args = append(args, f.HomeID)
		idx++
	}

	// Count total (filtrado). Sem filtro, usa o ranking_meta (mais barato).
	var totalCount int
	if f.CharacterToolName == "" && f.HomeID == 0 {
		err := r.pool.QueryRow(ctx,
			`SELECT total_count FROM ranking_meta WHERE ranking_type = $1`,
			string(rt)).Scan(&totalCount)
		if errors.Is(err, pgx.ErrNoRows) {
			totalCount = 0
		} else if err != nil {
			return nil, err
		}
	} else {
		err := r.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM ranking_entry WHERE `+where,
			args...,
		).Scan(&totalCount)
		if err != nil {
			return nil, err
		}
	}

	// Busca a página
	args = append(args, limit, offset)
	query := `
		SELECT ranking_type, order_no, short_id, fighter_id,
			character_id, character_tool_name, character_name,
			league_point, league_rank, master_league, master_rating,
			master_rating_order, home_id, platform_id, full_data
		FROM ranking_entry
		WHERE ` + where + `
		ORDER BY order_no ASC
		LIMIT $` + fmt.Sprintf("%d", idx) + ` OFFSET $` + fmt.Sprintf("%d", idx+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}

	totalPages := (totalCount + limit - 1) / limit
	return &domain.ListPage{
		Entries:     entries,
		CurrentPage: page,
		TotalPages:  totalPages,
		TotalCount:  totalCount,
		Limit:       limit,
	}, nil
}

// ByPlayer devolve todas as entries de um jogador em um ranking.
func (r *RankingRepository) ByPlayer(ctx context.Context, rt domain.RankingType, shortID int64) ([]domain.Entry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ranking_type, order_no, short_id, fighter_id,
			character_id, character_tool_name, character_name,
			league_point, league_rank, master_league, master_rating,
			master_rating_order, home_id, platform_id, full_data
		FROM ranking_entry
		WHERE ranking_type = $1 AND short_id = $2
		ORDER BY order_no ASC
	`, string(rt), shortID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// Around devolve entries em volta de uma posição (±radius).
func (r *RankingRepository) Around(ctx context.Context, rt domain.RankingType, order int, radius int) ([]domain.Entry, error) {
	if radius <= 0 {
		radius = 5
	}
	if radius > 100 {
		radius = 100
	}
	from := order - radius
	if from < 1 {
		from = 1
	}
	to := order + radius

	rows, err := r.pool.Query(ctx, `
		SELECT ranking_type, order_no, short_id, fighter_id,
			character_id, character_tool_name, character_name,
			league_point, league_rank, master_league, master_rating,
			master_rating_order, home_id, platform_id, full_data
		FROM ranking_entry
		WHERE ranking_type = $1 AND order_no BETWEEN $2 AND $3
		ORDER BY order_no ASC
	`, string(rt), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

// FacetsOf devolve contadores por personagem e por região.
// Pra performance em tabelas grandes, limita o top 50 por categoria.
func (r *RankingRepository) FacetsOf(ctx context.Context, rt domain.RankingType) (*domain.Facets, error) {
	out := &domain.Facets{Characters: []domain.CharacterCount{}, Homes: []domain.HomeCount{}}

	// Characters
	rows, err := r.pool.Query(ctx, `
		SELECT character_tool_name, character_name, COUNT(*) as c
		FROM ranking_entry
		WHERE ranking_type = $1 AND character_tool_name <> ''
		GROUP BY character_tool_name, character_name
		ORDER BY c DESC
		LIMIT 50
	`, string(rt))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cc domain.CharacterCount
		if err := rows.Scan(&cc.CharacterToolName, &cc.CharacterName, &cc.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out.Characters = append(out.Characters, cc)
	}
	rows.Close()

	// Homes
	rows, err = r.pool.Query(ctx, `
		SELECT home_id, COUNT(*) as c
		FROM ranking_entry
		WHERE ranking_type = $1 AND home_id > 0
		GROUP BY home_id
		ORDER BY c DESC
		LIMIT 50
	`, string(rt))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var hc domain.HomeCount
		if err := rows.Scan(&hc.HomeID, &hc.Count); err != nil {
			return nil, err
		}
		out.Homes = append(out.Homes, hc)
	}

	return out, nil
}

// scanEntries é helper compartilhado pelos métodos de query.
func scanEntries(rows pgx.Rows) ([]domain.Entry, error) {
	var entries []domain.Entry
	for rows.Next() {
		var e domain.Entry
		var rtStr string
		var fullData []byte
		if err := rows.Scan(
			&rtStr, &e.OrderNo, &e.ShortID, &e.FighterID,
			&e.CharacterID, &e.CharacterToolName, &e.CharacterName,
			&e.LeaguePoint, &e.LeagueRank, &e.MasterLeague, &e.MasterRating,
			&e.MasterRatingOrder, &e.HomeID, &e.PlatformID, &fullData,
		); err != nil {
			return nil, err
		}
		e.RankingType = domain.RankingType(rtStr)
		e.FullData = fullData
		entries = append(entries, e)
	}
	return entries, nil
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
