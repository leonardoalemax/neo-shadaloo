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

// fetchFightingEndpoint busca um endpoint específico (dia ou dia_master).
func (c *Client) fetchFightingEndpoint(ctx context.Context, endpoint, yyyymm string) ([]domain.LeagueFighting, error) {
	url := fmt.Sprintf("%s/api/en/stats/%s/%s", sf6Base, endpoint, yyyymm)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", fmt.Sprintf("%s/stats/%s/%s", sf6Base, endpoint, yyyymm))
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
		return nil, fmt.Errorf("SF6 %s/%s returned %d", endpoint, yyyymm, resp.StatusCode)
	}

	var result sf6DiaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("SF6 %s/%s decode error: %w", endpoint, yyyymm, err)
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

	return leagues, nil
}

// FetchFighting combina os endpoints "dia" (rankings normais) e "dia_master"
// (4 tiers acima de master) em paralelo. Tolera falha de um dos dois.
func (c *Client) FetchFighting(ctx context.Context, yyyymm string) ([]domain.LeagueFighting, error) {
	type result struct {
		leagues []domain.LeagueFighting
		err     error
	}

	regularCh := make(chan result, 1)
	masterCh := make(chan result, 1)

	go func() {
		l, err := c.fetchFightingEndpoint(ctx, "dia", yyyymm)
		regularCh <- result{l, err}
	}()
	go func() {
		l, err := c.fetchFightingEndpoint(ctx, "dia_master", yyyymm)
		masterCh <- result{l, err}
	}()

	regular := <-regularCh
	master := <-masterCh

	if regular.err != nil && master.err != nil {
		return nil, fmt.Errorf("SF6 fighting %s: ambos endpoints falharam: regular=%v, master=%v",
			yyyymm, regular.err, master.err)
	}
	if regular.err != nil {
		log.Printf("[sf6-fighting] %s: endpoint regular falhou: %v", yyyymm, regular.err)
	}
	if master.err != nil {
		log.Printf("[sf6-fighting] %s: endpoint master falhou: %v", yyyymm, master.err)
	}

	// Merge: master sobrescreve regular em caso de colisão por LeagueRank.
	masterRanks := make(map[int]bool, len(master.leagues))
	for _, l := range master.leagues {
		masterRanks[l.LeagueRank] = true
	}

	merged := make([]domain.LeagueFighting, 0, len(regular.leagues)+len(master.leagues))
	dropped := 0
	for _, l := range regular.leagues {
		if masterRanks[l.LeagueRank] {
			dropped++
			continue
		}
		merged = append(merged, l)
	}
	merged = append(merged, master.leagues...)

	log.Printf("[sf6-fighting] %s: %d leagues (regular=%d, master=%d, sobrescritos=%d)",
		yyyymm, len(merged), len(regular.leagues), len(master.leagues), dropped)
	return merged, nil
}
