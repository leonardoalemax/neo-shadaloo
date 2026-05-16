package ranking

import "encoding/json"

// RankingType representa qual ranking um entry pertence.
type RankingType string

const (
	RankingLeague  RankingType = "league_point"   // Pontos da Liga
	RankingArcade  RankingType = "arcade_score"   // Pontuação no Arcade
	RankingKudos   RankingType = "kudos"          // Kudos (PP)
	RankingMaster  RankingType = "master_rating"  // Avaliação de Mestre
)

// AllRankingTypes lista os rankings sincronizados pelo Service genérico.
// - RankingKudos: removido até descobrirmos a URL correta no SF6.
// - RankingLeague: tem seu próprio service (application/league) com tabela dedicada.
func AllRankingTypes() []RankingType {
	return []RankingType{RankingArcade, RankingMaster}
}

// Entry é uma posição de um jogador (com seu personagem) em um ranking.
// Os campos planos são os mais consultáveis; o resto fica em FullData (jsonb).
type Entry struct {
	RankingType        RankingType     `json:"ranking_type"`
	OrderNo            int             `json:"order"`           // posição global no ranking (1, 2, 3, ...)
	ShortID            int64           `json:"short_id"`
	FighterID          string          `json:"fighter_id"`
	CharacterID        int             `json:"character_id"`         // personagem usado nessa entrada
	CharacterToolName  string          `json:"character_tool_name"`
	CharacterName      string          `json:"character_name"`
	LeaguePoint        int             `json:"league_point"`
	LeagueRank         int             `json:"league_rank"`
	MasterLeague       int             `json:"master_league"`
	MasterRating       int             `json:"master_rating"`
	MasterRatingOrder  int             `json:"master_rating_ranking"`
	HomeID             int             `json:"home_id"`
	PlatformID         int             `json:"platform_id"`
	FullData           json.RawMessage `json:"full_data" swaggertype:"object"` // JSON completo do entry (para campos não-extraídos)
}

// SnapshotMeta é o metadado de uma sincronização (snapshot atual).
type SnapshotMeta struct {
	RankingType  RankingType `json:"ranking_type"`
	TotalCount   int         `json:"total_count"`
	TotalPages   int         `json:"total_pages"`
	SyncedPages  int         `json:"synced_pages"`
	UpdatedAt    int64       `json:"updated_at"`     // última atividade (atualizado a cada progresso)
	StartedAt    int64       `json:"started_at"`     // quando o sync atual/último começou
	LastSyncedAt int64       `json:"last_synced_at"` // quando o último sync COMPLETOU com sucesso
	Status       string      `json:"status"`         // "running", "done", "failed"
}
