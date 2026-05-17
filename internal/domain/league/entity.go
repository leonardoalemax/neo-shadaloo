package league

import "encoding/json"

// Player é um jogador no ranking de league. Diferente de ranking.Entry,
// a chave primária é short_id — players são upserted, nunca deletados.
// order_no representa apenas a última posição vista (pode ficar desatualizado).
type Player struct {
	ShortID           int64           `json:"short_id"`
	FighterID         string          `json:"fighter_id"`
	CharacterID       int             `json:"character_id"`
	CharacterToolName string          `json:"character_tool_name"`
	CharacterName     string          `json:"character_name"`
	LeaguePoint       int             `json:"league_point"`
	LeagueRank        int             `json:"league_rank"`
	MasterLeague      int             `json:"master_league"`
	MasterRating      int             `json:"master_rating"`
	HomeID            int             `json:"home_id"`
	PlatformID        int             `json:"platform_id"`
	OrderNo           int             `json:"order"`
	FullData          json.RawMessage `json:"full_data" swaggertype:"object"`
	UpdatedAt         int64           `json:"updated_at"`
}

// SyncMeta espelha o estado do sync (1 linha global em league_meta).
type SyncMeta struct {
	TotalCount   int   `json:"total_count"`
	TotalPages   int   `json:"total_pages"`
	SyncedPages  int   `json:"synced_pages"`
	UpdatedAt    int64 `json:"updated_at"`
	StartedAt    int64 `json:"started_at"`
	LastSyncedAt int64 `json:"last_synced_at"`
	Status       string `json:"status"`
}

// CountryPlayerCount conta players únicos por país (pro mapa).
type CountryPlayerCount struct {
	HomeID      int    `json:"home_id"`
	CountryName string `json:"country_name"`
	ISO3        string `json:"iso3"`
	PlayerCount int    `json:"player_count"`
}

// MapFilter reúne filtros opcionais para o endpoint players-by-country.
type MapFilter struct {
	Character string // character_tool_name (ex: "ryu")
	LeagueRank int   // league_rank exato (0 = sem filtro)
}

// CharacterCount representa um personagem e quantos players o usam.
type CharacterCount struct {
	CharacterToolName string `json:"character_tool_name"`
	CharacterName     string `json:"character_name"`
	PlayerCount       int    `json:"player_count"`
}
