package sf6

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	domain "neo-shadaloo/internal/domain/usage"
)

// sf6UsageCharEntry mirrors the nested val[] entry from the Buckler API.
type sf6UsageCharEntry struct {
	CharacterToolName string  `json:"character_tool_name"`
	CharacterAlpha    string  `json:"character_alpha"`
	PlayRate          float64 `json:"play_rate"`
	PreviousRate      float64 `json:"previous_rate"`
}

// sf6UsageLeague mirrors the inner val[] entry (one league).
type sf6UsageLeague struct {
	LeagueRank  int                 `json:"league_rank"`
	LeagueAlpha string              `json:"league_alpha"`
	Val         []sf6UsageCharEntry `json:"val"`
}

// sf6UsageGroup mirrors the top-level usagerateData[] entry.
type sf6UsageGroup struct {
	OperationType int              `json:"operation_type"`
	Val           []sf6UsageLeague `json:"val"`
}

type sf6UsageAPIResponse struct {
	UsagerateData []sf6UsageGroup `json:"usagerateData"`
}

// FetchUsage fetches character usage data for a given YYYYMM period.
func (c *Client) FetchUsage(ctx context.Context, yyyymm string) ([]domain.LeagueUsage, error) {
	url := fmt.Sprintf("%s/api/en/stats/usagerate/%s", sf6Base, yyyymm)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", fmt.Sprintf("%s/stats/usagerate/%s", sf6Base, yyyymm))
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("Cookie", os.Getenv("SF6_COOKIE"))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SF6 usage %s returned %d", yyyymm, resp.StatusCode)
	}

	var result sf6UsageAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("SF6 usage %s decode error: %w", yyyymm, err)
	}

	var leagues []domain.LeagueUsage
	for _, group := range result.UsagerateData {
		for _, league := range group.Val {
			entries := make([]domain.CharUsageEntry, len(league.Val))
			for i, e := range league.Val {
				entries[i] = domain.CharUsageEntry{
					CharacterToolName: e.CharacterToolName,
					CharacterAlpha:    e.CharacterAlpha,
					PlayRate:          e.PlayRate,
					PreviousRate:      e.PreviousRate,
				}
			}
			leagues = append(leagues, domain.LeagueUsage{
				OperationType: group.OperationType,
				LeagueRank:    league.LeagueRank,
				LeagueAlpha:   league.LeagueAlpha,
				Entries:       entries,
			})
		}
	}

	log.Printf("[sf6-usage] %s: %d leagues fetched", yyyymm, len(leagues))
	return leagues, nil
}
