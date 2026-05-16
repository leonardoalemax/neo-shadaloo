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

// fetchUsageEndpoint busca um endpoint específico (usagerate ou usagerate_master)
// e devolve as leagues parseadas.
func (c *Client) fetchUsageEndpoint(ctx context.Context, endpoint, yyyymm string) ([]domain.LeagueUsage, error) {
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

	var result sf6UsageAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("SF6 %s/%s decode error: %w", endpoint, yyyymm, err)
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

	return leagues, nil
}

// FetchUsage busca character usage data para um determinado YYYYMM, combinando
// os endpoints "usagerate" (rankings normais) e "usagerate_master" (4 tiers
// acima de master) em paralelo. Se um dos dois falhar, devolve apenas o que
// deu certo (com log do erro do outro). Só falha se ambos errarem.
func (c *Client) FetchUsage(ctx context.Context, yyyymm string) ([]domain.LeagueUsage, error) {
	type result struct {
		leagues []domain.LeagueUsage
		err     error
	}

	regularCh := make(chan result, 1)
	masterCh := make(chan result, 1)

	go func() {
		l, err := c.fetchUsageEndpoint(ctx, "usagerate", yyyymm)
		regularCh <- result{l, err}
	}()
	go func() {
		l, err := c.fetchUsageEndpoint(ctx, "usagerate_master", yyyymm)
		masterCh <- result{l, err}
	}()

	regular := <-regularCh
	master := <-masterCh

	if regular.err != nil && master.err != nil {
		return nil, fmt.Errorf("SF6 usage %s: ambos endpoints falharam: regular=%v, master=%v",
			yyyymm, regular.err, master.err)
	}
	if regular.err != nil {
		log.Printf("[sf6-usage] %s: endpoint regular falhou: %v", yyyymm, regular.err)
	}
	if master.err != nil {
		log.Printf("[sf6-usage] %s: endpoint master falhou: %v", yyyymm, master.err)
	}

	// Merge: master sobrescreve regular em caso de colisão por
	// (operation_type, league_alpha). Assim, se regular já tem MASTER,
	// a versão do endpoint master prevalece.
	type key struct {
		op    int
		alpha string
	}
	masterKeys := make(map[key]bool, len(master.leagues))
	for _, l := range master.leagues {
		masterKeys[key{l.OperationType, l.LeagueAlpha}] = true
	}

	merged := make([]domain.LeagueUsage, 0, len(regular.leagues)+len(master.leagues))
	dropped := 0
	for _, l := range regular.leagues {
		if masterKeys[key{l.OperationType, l.LeagueAlpha}] {
			dropped++
			continue
		}
		merged = append(merged, l)
	}
	merged = append(merged, master.leagues...)

	log.Printf("[sf6-usage] %s: %d leagues (regular=%d, master=%d, sobrescritos=%d)",
		yyyymm, len(merged), len(regular.leagues), len(master.leagues), dropped)
	return merged, nil
}
