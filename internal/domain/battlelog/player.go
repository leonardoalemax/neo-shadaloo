package battlelog

import "context"

// Player representa um jogador com seus dados mais recentes.
// Alimentado pelo BannerInfo (dono do battlelog) e PlayerInfo (adversários nos replays).
type Player struct {
	ShortID                  int64  `json:"short_id"`
	FighterID                string `json:"fighter_id"`
	PlatformID               int    `json:"platform_id"`
	PlatformName             string `json:"platform_name"`
	PlatformToolName         string `json:"platform_tool_name"`
	HomeID                   int    `json:"home_id"`
	FavoriteCharacterToolName string `json:"favorite_character_tool_name"`
	FavoriteCharacterName    string `json:"favorite_character_name"`
	LeaguePoint              int    `json:"league_point"`
	LeagueRank               int    `json:"league_rank"`
	TitlePlateName           string `json:"title_plate_name"`
	TitleVal                 string `json:"title_val"`
	PPFightingGround         int    `json:"pp_fighting_ground"`
	PPWorldTour              int    `json:"pp_world_tour"`
	PPBattleHub              int    `json:"pp_battle_hub"`
	UpdatedAt                int64  `json:"updated_at"`
	Syncable                 bool   `json:"syncable"`
}

// PlayerCharacter é um personagem usado por um jogador.
// Upsert por (short_id, character_tool_name). Mantém o dado mais recente.
type PlayerCharacter struct {
	ShortID           int64  `json:"short_id"`
	CharacterToolName string `json:"character_tool_name"`
	CharacterName     string `json:"character_name"`
	LeaguePoint       int    `json:"league_point"`
	LeagueRank        int    `json:"league_rank"`
	LastSeenAt        int64  `json:"last_seen_at"`
}

// PlayerRepository gerencia a tabela player + player_character.
type PlayerRepository interface {
	// UpsertFromBanner faz upsert completo do player (dados ricos do BannerInfo).
	UpsertFromBanner(ctx context.Context, p Player) error

	// UpsertFromReplay faz upsert parcial — só atualiza dados básicos,
	// NÃO sobrescreve campos ricos (title, kudos, favorite_character, home_id)
	// que só vêm do BannerInfo.
	UpsertFromReplay(ctx context.Context, players []Player) error

	// UpsertCharacters faz upsert dos personagens usados.
	UpsertCharacters(ctx context.Context, chars []PlayerCharacter) error

	// Search busca players por fighter_id (ILIKE).
	Search(ctx context.Context, query string) ([]Player, error)

	// MarkSyncable marca um player como syncable.
	MarkSyncable(ctx context.Context, fighterID string) error

	// ListSyncableUserIDs retorna fighter_ids de players syncable.
	ListSyncableUserIDs(ctx context.Context) ([]string, error)

	// ListAllUserIDs retorna todos os fighter_ids.
	ListAllUserIDs(ctx context.Context) ([]string, error)

	// GetByFighterID busca um player pelo fighter_id.
	GetByFighterID(ctx context.Context, fighterID string) (*Player, error)

	// GetCharacters retorna os personagens de um player.
	GetCharacters(ctx context.Context, shortID int64) ([]PlayerCharacter, error)
}
