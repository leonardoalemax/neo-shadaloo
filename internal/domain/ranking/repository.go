package ranking

import "context"

// Repository abstrai a persistência das entries e snapshots.
type Repository interface {
	// ReplaceAll substitui todos os entries de um ranking_type pelos novos.
	// Implementação típica: TRUNCATE + bulk INSERT em transação.
	ReplaceAll(ctx context.Context, rt RankingType, entries []Entry) error

	// AppendBatch adiciona um lote de entries durante o crawl em curso.
	// Útil pra ir persistindo página por página em vez de manter tudo em memória.
	AppendBatch(ctx context.Context, entries []Entry) error

	// ClearType apaga todos os entries de um ranking_type (chamado antes do AppendBatch).
	ClearType(ctx context.Context, rt RankingType) error

	// SaveMeta atualiza o estado da sincronização (running/done/failed + progresso).
	SaveMeta(ctx context.Context, meta SnapshotMeta) error

	// GetMeta retorna o último estado de sync de um ranking.
	GetMeta(ctx context.Context, rt RankingType) (*SnapshotMeta, error)
}
