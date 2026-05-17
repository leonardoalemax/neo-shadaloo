package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "neo-shadaloo/internal/domain/battlelog"
)

type playerRepository struct {
	pool *pgxpool.Pool
}

func NewPlayerRepository(pool *pgxpool.Pool) domain.PlayerRepository {
	return &playerRepository{pool: pool}
}

// UpsertFromBanner faz upsert completo — dados ricos vindos do BannerInfo.
func (r *playerRepository) UpsertFromBanner(ctx context.Context, p domain.Player) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO player (
			short_id, fighter_id, platform_id, platform_name, platform_tool_name,
			home_id, favorite_character_tool_name, favorite_character_name,
			league_point, league_rank, title_plate_name, title_val,
			pp_fighting_ground, pp_world_tour, pp_battle_hub,
			updated_at, syncable
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (short_id) DO UPDATE SET
			fighter_id                   = EXCLUDED.fighter_id,
			platform_id                  = EXCLUDED.platform_id,
			platform_name                = EXCLUDED.platform_name,
			platform_tool_name           = EXCLUDED.platform_tool_name,
			home_id                      = EXCLUDED.home_id,
			favorite_character_tool_name = EXCLUDED.favorite_character_tool_name,
			favorite_character_name      = EXCLUDED.favorite_character_name,
			league_point                 = EXCLUDED.league_point,
			league_rank                  = EXCLUDED.league_rank,
			title_plate_name             = EXCLUDED.title_plate_name,
			title_val                    = EXCLUDED.title_val,
			pp_fighting_ground           = EXCLUDED.pp_fighting_ground,
			pp_world_tour                = EXCLUDED.pp_world_tour,
			pp_battle_hub                = EXCLUDED.pp_battle_hub,
			updated_at                   = EXCLUDED.updated_at
	`, p.ShortID, p.FighterID, p.PlatformID, p.PlatformName, p.PlatformToolName,
		p.HomeID, p.FavoriteCharacterToolName, p.FavoriteCharacterName,
		p.LeaguePoint, p.LeagueRank, p.TitlePlateName, p.TitleVal,
		p.PPFightingGround, p.PPWorldTour, p.PPBattleHub,
		p.UpdatedAt, p.Syncable,
	)
	return err
}

// UpsertFromReplay faz upsert parcial — só atualiza dados básicos.
// NÃO sobrescreve campos ricos (title, kudos, favorite_character, home_id).
func (r *playerRepository) UpsertFromReplay(ctx context.Context, players []domain.Player) error {
	if len(players) == 0 {
		return nil
	}
	for _, p := range players {
		if p.ShortID == 0 {
			continue
		}
		_, err := r.pool.Exec(ctx, `
			INSERT INTO player (
				short_id, fighter_id, platform_id, platform_name, platform_tool_name,
				league_point, league_rank, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (short_id) DO UPDATE SET
				fighter_id         = EXCLUDED.fighter_id,
				platform_id        = EXCLUDED.platform_id,
				platform_name      = EXCLUDED.platform_name,
				platform_tool_name = EXCLUDED.platform_tool_name,
				league_point       = CASE WHEN EXCLUDED.updated_at > player.updated_at
				                     THEN EXCLUDED.league_point ELSE player.league_point END,
				league_rank        = CASE WHEN EXCLUDED.updated_at > player.updated_at
				                     THEN EXCLUDED.league_rank ELSE player.league_rank END,
				updated_at         = GREATEST(player.updated_at, EXCLUDED.updated_at)
		`, p.ShortID, p.FighterID, p.PlatformID, p.PlatformName, p.PlatformToolName,
			p.LeaguePoint, p.LeagueRank, p.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("upsert player short_id=%d: %w", p.ShortID, err)
		}
	}
	return nil
}

// UpsertCharacters faz upsert dos personagens usados.
func (r *playerRepository) UpsertCharacters(ctx context.Context, chars []domain.PlayerCharacter) error {
	if len(chars) == 0 {
		return nil
	}
	for _, c := range chars {
		if c.ShortID == 0 || c.CharacterToolName == "" {
			continue
		}
		_, err := r.pool.Exec(ctx, `
			INSERT INTO player_character (
				short_id, character_tool_name, character_name,
				league_point, league_rank, last_seen_at
			) VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (short_id, character_tool_name) DO UPDATE SET
				character_name = EXCLUDED.character_name,
				league_point   = CASE WHEN EXCLUDED.last_seen_at > player_character.last_seen_at
				                 THEN EXCLUDED.league_point ELSE player_character.league_point END,
				league_rank    = CASE WHEN EXCLUDED.last_seen_at > player_character.last_seen_at
				                 THEN EXCLUDED.league_rank ELSE player_character.league_rank END,
				last_seen_at   = GREATEST(player_character.last_seen_at, EXCLUDED.last_seen_at)
		`, c.ShortID, c.CharacterToolName, c.CharacterName,
			c.LeaguePoint, c.LeagueRank, c.LastSeenAt,
		)
		if err != nil {
			return fmt.Errorf("upsert player_character short_id=%d char=%s: %w", c.ShortID, c.CharacterToolName, err)
		}
	}
	return nil
}

func (r *playerRepository) Search(ctx context.Context, query string) ([]domain.Player, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT short_id, fighter_id, platform_id, platform_name, platform_tool_name,
		       home_id, favorite_character_tool_name, favorite_character_name,
		       league_point, league_rank, updated_at
		FROM player
		WHERE fighter_id ILIKE '%' || $1 || '%'
		ORDER BY updated_at DESC
		LIMIT 20
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Player
	for rows.Next() {
		var p domain.Player
		if err := rows.Scan(
			&p.ShortID, &p.FighterID, &p.PlatformID, &p.PlatformName, &p.PlatformToolName,
			&p.HomeID, &p.FavoriteCharacterToolName, &p.FavoriteCharacterName,
			&p.LeaguePoint, &p.LeagueRank, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *playerRepository) MarkSyncable(ctx context.Context, fighterID string) error {
	// Tenta marcar na tabela player. Se o player ainda não existe (short_id desconhecido),
	// mantém no player_index como fallback até o primeiro sync popular a tabela player.
	tag, err := r.pool.Exec(ctx, `
		UPDATE player SET syncable = true WHERE fighter_id = $1
	`, fighterID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Fallback: insere no player_index pra não perder o mark
		_, err = r.pool.Exec(ctx, `
			INSERT INTO player_index (fighter_id, short_id, character_tool_name, updated_at, syncable)
			VALUES ($1, 0, '', 0, true)
			ON CONFLICT (fighter_id) DO UPDATE SET syncable = true
		`, fighterID)
		return err
	}
	return nil
}

func (r *playerRepository) ListSyncableUserIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT fighter_id FROM player WHERE syncable = true
		UNION
		SELECT fighter_id FROM player_index WHERE syncable = true
	`)
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

func (r *playerRepository) ListAllUserIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT fighter_id FROM player`)
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

func (r *playerRepository) GetByFighterID(ctx context.Context, fighterID string) (*domain.Player, error) {
	var p domain.Player
	err := r.pool.QueryRow(ctx, `
		SELECT short_id, fighter_id, platform_id, platform_name, platform_tool_name,
		       home_id, favorite_character_tool_name, favorite_character_name,
		       league_point, league_rank, title_plate_name, title_val,
		       pp_fighting_ground, pp_world_tour, pp_battle_hub,
		       updated_at, syncable
		FROM player
		WHERE fighter_id = $1
	`, fighterID).Scan(
		&p.ShortID, &p.FighterID, &p.PlatformID, &p.PlatformName, &p.PlatformToolName,
		&p.HomeID, &p.FavoriteCharacterToolName, &p.FavoriteCharacterName,
		&p.LeaguePoint, &p.LeagueRank, &p.TitlePlateName, &p.TitleVal,
		&p.PPFightingGround, &p.PPWorldTour, &p.PPBattleHub,
		&p.UpdatedAt, &p.Syncable,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *playerRepository) GetCharacters(ctx context.Context, shortID int64) ([]domain.PlayerCharacter, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT short_id, character_tool_name, character_name,
		       league_point, league_rank, last_seen_at
		FROM player_character
		WHERE short_id = $1
		ORDER BY last_seen_at DESC
	`, shortID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PlayerCharacter
	for rows.Next() {
		var c domain.PlayerCharacter
		if err := rows.Scan(&c.ShortID, &c.CharacterToolName, &c.CharacterName,
			&c.LeaguePoint, &c.LeagueRank, &c.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
