package battlelog

import "context"

// BattlelogRepository defines persistence operations for the Battlelog aggregate.
type BattlelogRepository interface {
	GetByUserID(ctx context.Context, userID string) (*Battlelog, error)
	Save(ctx context.Context, b *Battlelog) error
}
