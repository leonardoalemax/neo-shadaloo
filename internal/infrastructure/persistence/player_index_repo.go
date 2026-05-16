package persistence

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "neo-shadaloo/internal/domain/battlelog"
)

type playerIndexRepository struct {
	pool *pgxpool.Pool
}

func NewPlayerIndexRepository(pool *pgxpool.Pool) domain.PlayerIndexRepository {
	return &playerIndexRepository{pool: pool}
}

func (r *playerIndexRepository) Upsert(ctx context.Context, players []domain.PlayerEntry) error {
	if len(players) == 0 {
		return nil
	}
	for _, p := range players {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO player_index (fighter_id, short_id, character_tool_name, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (fighter_id) DO UPDATE SET
				short_id           = EXCLUDED.short_id,
				character_tool_name = EXCLUDED.character_tool_name,
				updated_at         = EXCLUDED.updated_at
		`, p.FighterID, p.ShortID, p.CharacterToolName, p.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpsertPreserveCharacter insere players novos normalmente, mas em conflito
// (player já existe) só atualiza short_id e updated_at. O character_tool_name
// original é preservado — útil pra ranking sync que vê o player com personagem
// diferente do favorito.
func (r *playerIndexRepository) UpsertPreserveCharacter(ctx context.Context, players []domain.PlayerEntry) error {
	if len(players) == 0 {
		return nil
	}
	for _, p := range players {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO player_index (fighter_id, short_id, character_tool_name, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (fighter_id) DO UPDATE SET
				short_id   = EXCLUDED.short_id,
				updated_at = EXCLUDED.updated_at
		`, p.FighterID, p.ShortID, p.CharacterToolName, p.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *playerIndexRepository) Search(ctx context.Context, query string) ([]domain.PlayerEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT fighter_id, short_id, character_tool_name, updated_at
		FROM player_index
		WHERE fighter_id ILIKE '%' || $1 || '%'
		ORDER BY updated_at DESC
		LIMIT 20
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.PlayerEntry
	for rows.Next() {
		var p domain.PlayerEntry
		if err := rows.Scan(&p.FighterID, &p.ShortID, &p.CharacterToolName, &p.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, nil
}

func (r *playerIndexRepository) ListAllUserIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT fighter_id FROM player_index`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *playerIndexRepository) ListSyncableUserIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT fighter_id FROM player_index WHERE syncable = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *playerIndexRepository) MarkSyncable(ctx context.Context, fighterID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO player_index (fighter_id, short_id, character_tool_name, updated_at, syncable)
		VALUES ($1, 0, '', 0, true)
		ON CONFLICT (fighter_id) DO UPDATE SET syncable = true
	`, fighterID)
	return err
}
