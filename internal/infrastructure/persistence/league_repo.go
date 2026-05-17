package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "neo-shadaloo/internal/domain/league"
)

type LeagueRepository struct {
	pool *pgxpool.Pool
}

func NewLeagueRepository(pool *pgxpool.Pool) *LeagueRepository {
	return &LeagueRepository{pool: pool}
}

// UpsertBatch insere/atualiza players por short_id em uma transação.
func (r *LeagueRepository) UpsertBatch(ctx context.Context, players []domain.Player) error {
	if len(players) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const stmt = `
		INSERT INTO league_player (
			short_id, fighter_id, character_id, character_tool_name, character_name,
			league_point, league_rank, master_league, master_rating,
			home_id, platform_id, order_no, full_data, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (short_id) DO UPDATE SET
			fighter_id          = EXCLUDED.fighter_id,
			character_id        = EXCLUDED.character_id,
			character_tool_name = EXCLUDED.character_tool_name,
			character_name      = EXCLUDED.character_name,
			league_point        = EXCLUDED.league_point,
			league_rank         = EXCLUDED.league_rank,
			master_league       = EXCLUDED.master_league,
			master_rating       = EXCLUDED.master_rating,
			home_id             = EXCLUDED.home_id,
			platform_id         = EXCLUDED.platform_id,
			order_no            = EXCLUDED.order_no,
			full_data           = EXCLUDED.full_data,
			updated_at          = EXCLUDED.updated_at
	`

	for _, p := range players {
		if p.ShortID == 0 {
			continue // sem short_id não dá pra upsert
		}
		if _, err := tx.Exec(ctx, stmt,
			p.ShortID, p.FighterID, p.CharacterID, p.CharacterToolName, p.CharacterName,
			p.LeaguePoint, p.LeagueRank, p.MasterLeague, p.MasterRating,
			p.HomeID, p.PlatformID, p.OrderNo, []byte(p.FullData), p.UpdatedAt,
		); err != nil {
			return fmt.Errorf("upsert league_player short_id=%d: %w", p.ShortID, err)
		}
	}
	return tx.Commit(ctx)
}

func (r *LeagueRepository) SaveMeta(ctx context.Context, m domain.SyncMeta) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE league_meta SET
			total_count    = $1,
			total_pages    = $2,
			synced_pages   = $3,
			updated_at     = $4,
			started_at     = CASE WHEN $5 > 0 THEN $5 ELSE started_at END,
			last_synced_at = CASE WHEN $6 > 0 THEN $6 ELSE last_synced_at END,
			status         = $7
		WHERE id = 1
	`,
		m.TotalCount, m.TotalPages, m.SyncedPages,
		m.UpdatedAt, m.StartedAt, m.LastSyncedAt, m.Status,
	)
	return err
}

func (r *LeagueRepository) GetMeta(ctx context.Context) (*domain.SyncMeta, error) {
	var m domain.SyncMeta
	err := r.pool.QueryRow(ctx, `
		SELECT total_count, total_pages, synced_pages, updated_at,
		       started_at, last_synced_at, status
		FROM league_meta WHERE id = 1
	`).Scan(&m.TotalCount, &m.TotalPages, &m.SyncedPages, &m.UpdatedAt,
		&m.StartedAt, &m.LastSyncedAt, &m.Status)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *LeagueRepository) PlayersByCountry(ctx context.Context, f domain.MapFilter) ([]domain.CountryPlayerCount, error) {
	where := "lp.home_id > 0"
	args := []any{}
	idx := 1

	if f.Character != "" {
		where += fmt.Sprintf(" AND lp.character_tool_name = $%d", idx)
		args = append(args, f.Character)
		idx++
	}
	if f.LeagueRank > 0 {
		where += fmt.Sprintf(" AND lp.league_rank = $%d", idx)
		args = append(args, f.LeagueRank)
		idx++
	}

	q := fmt.Sprintf(`
		SELECT lp.home_id,
		       COALESCE(hc.name, '') AS country_name,
		       COALESCE(hc.iso3, '') AS iso3,
		       COUNT(DISTINCT lp.short_id) AS player_count
		FROM league_player lp
		LEFT JOIN home_country hc ON hc.home_id = lp.home_id
		WHERE %s
		GROUP BY lp.home_id, hc.name, hc.iso3
		ORDER BY player_count DESC
	`, where)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CountryPlayerCount
	for rows.Next() {
		var c domain.CountryPlayerCount
		if err := rows.Scan(&c.HomeID, &c.CountryName, &c.ISO3, &c.PlayerCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *LeagueRepository) DistinctCharacters(ctx context.Context) ([]domain.CharacterCount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT character_tool_name, character_name, COUNT(*) AS player_count
		FROM league_player
		WHERE character_tool_name <> ''
		GROUP BY character_tool_name, character_name
		ORDER BY player_count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.CharacterCount
	for rows.Next() {
		var c domain.CharacterCount
		if err := rows.Scan(&c.CharacterToolName, &c.CharacterName, &c.PlayerCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *LeagueRepository) DistinctRanks(ctx context.Context) ([]domain.RankCount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT league_rank, COUNT(*) AS player_count
		FROM league_player
		WHERE league_rank > 0
		GROUP BY league_rank
		ORDER BY league_rank ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RankCount
	for rows.Next() {
		var c domain.RankCount
		if err := rows.Scan(&c.LeagueRank, &c.PlayerCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *LeagueRepository) CountPlayers(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM league_player`).Scan(&n)
	return n, err
}
