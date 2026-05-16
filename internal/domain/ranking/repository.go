package ranking

import "context"

// ListFilter representa filtros opcionais pra listagem paginada.
// Strings vazias / zero significam "sem filtro".
type ListFilter struct {
	CharacterToolName string
	HomeID            int
	Page              int // 1-based
	Limit             int // default 50, max 200
}

// CharacterCount agrega o número de entradas por personagem.
type CharacterCount struct {
	CharacterToolName string `json:"character_tool_name"`
	CharacterName     string `json:"character_name"`
	Count             int    `json:"count"`
}

// HomeCount agrega o número de entradas por região (home_id).
type HomeCount struct {
	HomeID int `json:"home_id"`
	Count  int `json:"count"`
}

// Facets agrupa os contadores pra preencher filtros no front.
type Facets struct {
	Characters []CharacterCount `json:"characters"`
	Homes      []HomeCount      `json:"homes"`
}

// Page é a resposta paginada de uma listagem.
type ListPage struct {
	Entries     []Entry `json:"entries"`
	CurrentPage int     `json:"current_page"`
	TotalPages  int     `json:"total_pages"`
	TotalCount  int     `json:"total_count"`
	Limit       int     `json:"limit"`
}

// Repository abstrai a persistência das entries e snapshots.
type Repository interface {
	// ReplaceAll substitui todos os entries de um ranking_type pelos novos.
	ReplaceAll(ctx context.Context, rt RankingType, entries []Entry) error

	// AppendBatch adiciona um lote de entries durante o crawl em curso.
	AppendBatch(ctx context.Context, entries []Entry) error

	// ClearType apaga todos os entries de um ranking_type (chamado antes do AppendBatch).
	ClearType(ctx context.Context, rt RankingType) error

	// SaveMeta atualiza o estado da sincronização (running/done/failed + progresso).
	SaveMeta(ctx context.Context, meta SnapshotMeta) error

	// GetMeta retorna o último estado de sync de um ranking.
	GetMeta(ctx context.Context, rt RankingType) (*SnapshotMeta, error)

	// ── Queries de visualização ────────────────────────────────────────────

	// List devolve uma página de entries ordenadas por order_no ASC.
	List(ctx context.Context, rt RankingType, f ListFilter) (*ListPage, error)

	// ByPlayer devolve todas as entries de um jogador em um ranking
	// (pode ter mais de uma se ele aparece com personagens diferentes).
	ByPlayer(ctx context.Context, rt RankingType, shortID int64) ([]Entry, error)

	// Around devolve N entries antes e depois de uma posição específica.
	Around(ctx context.Context, rt RankingType, order int, radius int) ([]Entry, error)

	// FacetsOf devolve contadores por personagem e por região pra preencher filtros.
	FacetsOf(ctx context.Context, rt RankingType) (*Facets, error)
}
