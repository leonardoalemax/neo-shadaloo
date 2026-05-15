package sf6

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	domain "neo-shadaloo/internal/domain/fighting"
)

type sf6FightingOpponent struct {
	ID        int     `json:"id"`
	NameAlpha string  `json:"name_alpha"`
	ToolName  string  `json:"tool_name"`
	InputType string  `json:"input_type"`
	Dsort     float64 `json:"_dsort"`
}

type sf6FightingValue struct {
	OID   int     `json:"_oid"`
	Dsort float64 `json:"_dsort"`
	SF    int     `json:"sf"`
	Thm   int     `json:"thm"`
	Val   string  `json:"val"`
}

type sf6FightingRecord struct {
	ID        int                `json:"id"`
	NameAlpha string             `json:"name_alpha"`
	ToolName  string             `json:"tool_name"`
	InputType string             `json:"input_type"`
	Total     string             `json:"total"`
	WinRate   float64            `json:"_win_rate"`
	Values    []sf6FightingValue `json:"values"`
}

type sf6LeagueData struct {
	OpponentHeader []sf6FightingOpponent `json:"opponent_header"`
	Records        []sf6FightingRecord   `json:"records"`
}

type sf6DiaResponse struct {
	DiaData struct {
		CI struct {
			CISort map[string]sf6LeagueData `json:"ci_sort"`
		} `json:"ci"`
	} `json:"diaData"`
}

func (c *Client) FetchFighting(ctx context.Context, yyyymm string) ([]domain.LeagueFighting, error) {
	url := fmt.Sprintf("%s/api/en/stats/dia/%s", sf6Base, yyyymm)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", fmt.Sprintf("%s/stats/dia/%s", sf6Base, yyyymm))
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
		return nil, fmt.Errorf("SF6 dia %s returned %d", yyyymm, resp.StatusCode)
	}

	var result sf6DiaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("SF6 dia %s decode error: %w", yyyymm, err)
	}

	leagues := make([]domain.LeagueFighting, 0, len(result.DiaData.CI.CISort))
	for rankStr, leagueData := range result.DiaData.CI.CISort {
		rank, _ := strconv.Atoi(rankStr)

		header := make([]domain.OpponentHeader, len(leagueData.OpponentHeader))
		for i, h := range leagueData.OpponentHeader {
			header[i] = domain.OpponentHeader{
				ID:        h.ID,
				NameAlpha: h.NameAlpha,
				ToolName:  h.ToolName,
				InputType: h.InputType,
			}
		}

		records := make([]domain.FightingRecord, len(leagueData.Records))
		for i, r := range leagueData.Records {
			values := make([]domain.FightingValue, len(r.Values))
			for j, v := range r.Values {
				values[j] = domain.FightingValue{OID: v.OID, Val: v.Val, SF: v.SF}
			}
			records[i] = domain.FightingRecord{
				ID:        r.ID,
				NameAlpha: r.NameAlpha,
				ToolName:  r.ToolName,
				InputType: r.InputType,
				Total:     r.Total,
				WinRate:   r.WinRate,
				Values:    values,
			}
		}

		leagues = append(leagues, domain.LeagueFighting{
			LeagueRank:     rank,
			OpponentHeader: header,
			Records:        records,
		})
	}

	log.Printf("[sf6-fighting] %s: %d leagues fetched", yyyymm, len(leagues))
	return leagues, nil
}
