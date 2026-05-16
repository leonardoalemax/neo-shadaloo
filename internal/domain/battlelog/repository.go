package battlelog

import "context"

// BattlelogRepository defines persistence operations for the Battlelog aggregate.
type BattlelogRepository interface {
	GetByUserID(ctx context.Context, userID string) (*Battlelog, error)
	Save(ctx context.Context, b *Battlelog) error
	ListAllUserIDs(ctx context.Context) ([]string, error)
}

// PlayerEntry is a lightweight record in the player search index.
type PlayerEntry struct {
	FighterID         string `json:"fighter_id"`
	ShortID           int64  `json:"short_id"`
	CharacterToolName string `json:"character_tool_name"`
	UpdatedAt         int64  `json:"updated_at"`
}

// PlayerIndexRepository manages the searchable player name→ID index.
type PlayerIndexRepository interface {
	// Upsert insere novos players e atualiza TODOS os campos dos existentes.
	// Usado pelo battlelog sync (fonte autoritativa do personagem favorito).
	Upsert(ctx context.Context, players []PlayerEntry) error

	// UpsertPreserveCharacter insere novos players, mas em players existentes
	// só atualiza short_id e updated_at — preserva o character_tool_name original.
	// Usado pelo ranking sync (onde o personagem é o do ranking, não o favorito).
	UpsertPreserveCharacter(ctx context.Context, players []PlayerEntry) error

	Search(ctx context.Context, query string) ([]PlayerEntry, error)
	ListAllUserIDs(ctx context.Context) ([]string, error)
	ListSyncableUserIDs(ctx context.Context) ([]string, error)
	MarkSyncable(ctx context.Context, fighterID string) error
}
