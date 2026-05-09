package battlelog

// BattlelogSyncedEvent is published after a successful sync for a user.
type BattlelogSyncedEvent struct {
	UserID   string
	CachedAt int64
}

// EventPublisher delivers domain events to interested subscribers.
// The concrete implementation (WebSocket hub) lives in infrastructure/realtime.
type EventPublisher interface {
	Publish(event BattlelogSyncedEvent)
}
