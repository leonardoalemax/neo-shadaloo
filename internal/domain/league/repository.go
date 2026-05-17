package league

import "context"

// Repository persiste os players e o meta do sync.
type Repository interface {
	// UpsertBatch insere players novos e atualiza os existentes (PK: short_id).
	UpsertBatch(ctx context.Context, players []Player) error

	// SaveMeta atualiza o meta global do sync.
	SaveMeta(ctx context.Context, m SyncMeta) error

	// GetMeta lê o meta atual.
	GetMeta(ctx context.Context) (*SyncMeta, error)

	// PlayersByCountry conta DISTINCT short_id por país (pro mapa), com filtros opcionais.
	PlayersByCountry(ctx context.Context, f MapFilter) ([]CountryPlayerCount, error)

	// DistinctCharacters retorna personagens distintos com contagem de players.
	DistinctCharacters(ctx context.Context) ([]CharacterCount, error)

	// CountPlayers retorna o total de players salvos (pra status).
	CountPlayers(ctx context.Context) (int, error)
}
