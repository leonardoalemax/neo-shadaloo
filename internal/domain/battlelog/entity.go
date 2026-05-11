package battlelog

import (
	"sort"
	"time"
)

// ── Value Objects ────────────────────────────────────────────────────────────

type PlayerRef struct {
	FighterID        string `json:"fighter_id"`
	PlatformID       int    `json:"platform_id"`
	ShortID          int64  `json:"short_id"`
	PlatformName     string `json:"platform_name"`
	PlatformToolName string `json:"platform_tool_name"`
}

type PlayerInfo struct {
	Player                   PlayerRef `json:"player"`
	CharacterID              int       `json:"character_id"`
	PlayingCharacterID       int       `json:"playing_character_id"`
	CharacterName            string    `json:"character_name"`
	CharacterToolName        string    `json:"character_tool_name"`
	PlayingCharacterName     string    `json:"playing_character_name"`
	PlayingCharacterToolName string    `json:"playing_character_tool_name"`
	LeaguePoint              int       `json:"league_point"`
	LeagueRank               int       `json:"league_rank"`
	BattleInputType          int       `json:"battle_input_type"`
	BattleInputTypeName      string    `json:"battle_input_type_name"`
	RoundResults             []int     `json:"round_results"`
	AllowCrossPlay           bool      `json:"allow_cross_play"`
}

type Replay struct {
	ReplayID                string     `json:"replay_id"`
	UploadedAt              int64      `json:"uploaded_at"`
	Player1Info             PlayerInfo `json:"player1_info"`
	Player2Info             PlayerInfo `json:"player2_info"`
	Views                   int        `json:"views"`
	ReplayBattleType        int        `json:"replay_battle_type"`
	ReplayBattleTypeName    string     `json:"replay_battle_type_name"`
	ReplayBattleSubTypeName string     `json:"replay_battle_sub_type_name"`
}

type LeagueRankInfo struct {
	LeagueRankName   string `json:"league_rank_name"`
	LeagueRankNumber int    `json:"league_rank_number"`
}

type FavoriteCharacterLeagueInfo struct {
	LeaguePoint         int            `json:"league_point"`
	LeagueRank          int            `json:"league_rank"`
	LeagueRankInfo      LeagueRankInfo `json:"league_rank_info"`
	MasterLeague        int            `json:"master_league"`
	MasterRating        int            `json:"master_rating"`
	MasterRatingRanking int            `json:"master_rating_ranking"`
}

type PlayPoint struct {
	FightingGround int `json:"fighting_ground"`
	WorldTour      int `json:"world_tour"`
	BattleHub      int `json:"battle_hub"`
}

type TitleData struct {
	TitleDataPlateName string `json:"title_data_plate_name"`
	TitleDataVal       string `json:"title_data_val"`
}

type ProfileComment struct {
	ProfileTagName string `json:"profile_tag_name"`
}

type PersonalInfo struct {
	FighterID        string `json:"fighter_id"`
	ShortID          int64  `json:"short_id"`
	PlatformName     string `json:"platform_name"`
	PlatformToolName string `json:"platform_tool_name"`
}

type FighterBannerInfo struct {
	PersonalInfo                PersonalInfo                `json:"personal_info"`
	FavoriteCharacterName       string                      `json:"favorite_character_name"`
	FavoriteCharacterAlpha      string                      `json:"favorite_character_alpha"`
	FavoriteCharacterToolName   string                      `json:"favorite_character_tool_name"`
	FavoriteCharacterLeagueInfo FavoriteCharacterLeagueInfo `json:"favorite_character_league_info"`
	FavoriteCharacterPlayPoint  PlayPoint                   `json:"favorite_character_play_point"`
	TitleData                   TitleData                   `json:"title_data"`
	ProfileComment              ProfileComment              `json:"profile_comment"`
}

// ReplayPage is the paginated response for the replays endpoint.
type ReplayPage struct {
	Replays    []Replay `json:"replays"`
	Total      int      `json:"total"`
	Page       int      `json:"page"`
	TotalPages int      `json:"total_pages"`
}

// ── Aggregate Root ───────────────────────────────────────────────────────────

const staleTTL = 5 * 60 * 1000 // 5 minutes in milliseconds

type Battlelog struct {
	UserID     string             `json:"userId"`
	Replays    []Replay           `json:"replays"`
	BannerInfo *FighterBannerInfo `json:"bannerInfo,omitempty"`
	CachedAt   int64              `json:"cachedAt"`
}

// IsStale reports whether the cached data is older than 5 minutes.
func (b *Battlelog) IsStale() bool {
	return time.Now().UnixMilli()-b.CachedAt > staleTTL
}

// HasReplay returns true if the given replayID already exists in this battlelog.
func (b *Battlelog) HasReplay(replayID string) bool {
	for _, r := range b.Replays {
		if r.ReplayID == replayID {
			return true
		}
	}
	return false
}

// MergeWith merges fresh replays into the aggregate, with fresh data taking
// precedence over existing entries. Returns true if any new replays were added.
func (b *Battlelog) MergeWith(fresh []Replay, banner *FighterBannerInfo) bool {
	merged := make(map[string]Replay, len(b.Replays)+len(fresh))
	for _, r := range b.Replays {
		merged[r.ReplayID] = r
	}

	hasNew := false
	for _, r := range fresh {
		if _, exists := merged[r.ReplayID]; !exists {
			hasNew = true
		}
		merged[r.ReplayID] = r
	}

	result := make([]Replay, 0, len(merged))
	for _, r := range merged {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UploadedAt > result[j].UploadedAt
	})

	b.Replays = result
	if banner != nil {
		b.BannerInfo = banner
	}
	b.CachedAt = time.Now().UnixMilli()
	return hasNew
}

// TouchCachedAt refreshes the cache timestamp without changing replay data.
func (b *Battlelog) TouchCachedAt() {
	b.CachedAt = time.Now().UnixMilli()
}
