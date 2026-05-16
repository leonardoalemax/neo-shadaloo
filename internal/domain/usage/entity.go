package usage

import "context"

// CharUsageEntry is one character's usage rate within a league group.
type CharUsageEntry struct {
	CharacterToolName string  `json:"character_tool_name"`
	CharacterAlpha    string  `json:"character_alpha"`
	PlayRate          float64 `json:"play_rate"`
	PreviousRate      float64 `json:"previous_rate"`
}

// LeagueUsage holds usage entries for one league rank group within one operation type.
type LeagueUsage struct {
	OperationType int              `json:"operation_type"`
	LeagueRank    int              `json:"league_rank"`
	LeagueAlpha   string           `json:"league_alpha"`
	Entries       []CharUsageEntry `json:"entries"`
}

// UsageSnapshot holds the usage data for one YYYYMM period.
type UsageSnapshot struct {
	YYYYMM   string        `json:"yyyymm"`
	Leagues  []LeagueUsage `json:"leagues"`
	CachedAt int64         `json:"cached_at"`
}

// UsageRepository is the persistence port for usage snapshots.
type UsageRepository interface {
	Get(ctx context.Context, yyyymm string) (*UsageSnapshot, error)
	Save(ctx context.Context, s *UsageSnapshot) error
	ListMonths(ctx context.Context) ([]string, error)
}

// UsageClient is the SF6 API port for fetching usage data.
type UsageClient interface {
	FetchUsage(ctx context.Context, yyyymm string) ([]LeagueUsage, error)
}
