package fighting

import "context"

type OpponentHeader struct {
	ID        int    `json:"id"`
	NameAlpha string `json:"name_alpha"`
	ToolName  string `json:"tool_name"`
	InputType string `json:"input_type"`
}

type FightingValue struct {
	OID int    `json:"oid"`
	Val string `json:"val"`
	SF  int    `json:"sf"`
}

type FightingRecord struct {
	ID        int             `json:"id"`
	NameAlpha string          `json:"name_alpha"`
	ToolName  string          `json:"tool_name"`
	InputType string          `json:"input_type"`
	Total     string          `json:"total"`
	WinRate   float64         `json:"win_rate"`
	Values    []FightingValue `json:"values"`
}

type LeagueFighting struct {
	LeagueRank     int              `json:"league_rank"`
	OpponentHeader []OpponentHeader `json:"opponent_header"`
	Records        []FightingRecord `json:"records"`
}

type FightingSnapshot struct {
	YYYYMM   string           `json:"yyyymm"`
	Leagues  []LeagueFighting `json:"leagues"`
	CachedAt int64            `json:"cached_at"`
}

type FightingRepository interface {
	Get(ctx context.Context, yyyymm string) (*FightingSnapshot, error)
	Save(ctx context.Context, s *FightingSnapshot) error
	ListMonths(ctx context.Context) ([]string, error)
}

type FightingClient interface {
	FetchFighting(ctx context.Context, yyyymm string) ([]LeagueFighting, error)
}
