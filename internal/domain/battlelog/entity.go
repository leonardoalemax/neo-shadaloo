package battlelog

import (
	"fmt"
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
	HomeID                      int                         `json:"home_id"`
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

// ReplayFilter holds optional filters for the replays endpoint.
type ReplayFilter struct {
	Character  string // playing_character_tool_name of the user
	DateFrom   int64  // unix seconds, inclusive (0 = no limit)
	DateTo     int64  // unix seconds, inclusive (0 = no limit)
	BattleType int    // 0 = all, 1 = ranked, 2 = casual, 3 = battle hub, 4 = custom room
}

// CharacterOption is a unique character played by a user.
type CharacterOption struct {
	ToolName string `json:"tool_name"`
	Name     string `json:"name"`
}

// CharacterRankStat holds the most recent LP and rank for a character.
type CharacterRankStat struct {
	Name       string `json:"name"`
	ToolName   string `json:"tool_name"`
	LP         int    `json:"lp"`
	LeagueRank int    `json:"league_rank"`
}

// ── Computed stat types ──────────────────────────────────────────────────────

type WinLossStat struct {
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
	Total  int `json:"total"`
	WinPct int `json:"win_pct"`
}

type CharStat struct {
	Name          string  `json:"name"`
	ToolName      string  `json:"tool_name"`
	Total         int     `json:"total"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	CleanLosses   int     `json:"clean_losses"`
	CloseLosses   int     `json:"close_losses"`
	WinRate       int     `json:"win_rate"`
	PriorityScore float64 `json:"priority_score"`
}

// TrainingSuggestion is an enriched opponent recommendation combining
// personal battle stats, global usage rate, and official matchup data.
type TrainingSuggestion struct {
	Name          string  `json:"name"`
	ToolName      string  `json:"tool_name"`
	Total         int     `json:"total"`           // personal battles against this char
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	CleanLosses   int     `json:"clean_losses"`
	CloseLosses   int     `json:"close_losses"`
	WinRate       int     `json:"win_rate"`         // personal win rate
	UsageRate     float64 `json:"usage_rate"`       // global usage rate in player's league (0–100)
	MatchupWR     float64 `json:"matchup_wr"`       // official matchup WR for player's char vs this (0–100, 0 if unknown)
	PriorityScore float64 `json:"priority_score"`   // final weighted score
}

type DayStat struct {
	Wins  int `json:"wins"`
	Total int `json:"total"`
}

type CalendarStat struct {
	ByDay     map[string]DayStat `json:"by_day"`
	ByWeekday [7]DayStat         `json:"by_weekday"`
}

// ── Replay helpers ───────────────────────────────────────────────────────────

// FindUserSide returns 1 or 2. userID may be the short_id stringified or the fighter_id.
func FindUserSide(r Replay, userID string) int {
	if fmt.Sprintf("%d", r.Player1Info.Player.ShortID) == userID ||
		r.Player1Info.Player.FighterID == userID {
		return 1
	}
	if fmt.Sprintf("%d", r.Player2Info.Player.ShortID) == userID ||
		r.Player2Info.Player.FighterID == userID {
		return 2
	}
	return 0
}

// GetWinner returns 1 or 2. Any round_result > 0 counts as a round won.
func GetWinner(r Replay) int {
	p1, p2 := 0, 0
	for _, v := range r.Player1Info.RoundResults {
		if v > 0 {
			p1++
		}
	}
	for _, v := range r.Player2Info.RoundResults {
		if v > 0 {
			p2++
		}
	}
	if p1 > p2 {
		return 1
	}
	return 2
}

// HourStat holds win/loss counts for a single hour of the day (0–23).
type HourStat struct {
	Hour  int `json:"hour"`
	Wins  int `json:"wins"`
	Total int `json:"total"`
}

// HourlyStats is the response shape for the hourly win-rate heatmap endpoint.
type HourlyStats struct {
	Hours [24]HourStat `json:"hours"`
}

// WeeklyHeatmap holds win/loss per (weekday, hour) — 7 days × 24 hours.
type WeeklyHeatmap struct {
	// Days[0]=Dom, Days[1]=Seg, ..., Days[6]=Sáb
	Days [7][24]HourStat `json:"days"`
}

// LPEntry holds the LP value for a given day (last recorded match of that day).
type LPEntry struct {
	Date string `json:"date"` // YYYY-MM-DD
	LP   int    `json:"lp"`
}

// LPHistory is the response shape for the LP evolution endpoint.
type LPHistory struct {
	Entries []LPEntry `json:"entries"`
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
