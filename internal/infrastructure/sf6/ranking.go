package sf6

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	domain "neo-shadaloo/internal/domain/ranking"
)

// rankingPath retorna o segmento de URL para cada tipo de ranking.
// As URLs reais (não-league) ainda não foram confirmadas pelo usuário —
// se alguma 404, basta ajustar aqui.
func rankingPath(rt domain.RankingType) string {
	switch rt {
	case domain.RankingLeague:
		return "league"
	case domain.RankingArcade:
		return "arcade"
	case domain.RankingKudos:
		return "point"
	case domain.RankingMaster:
		return "master"
	}
	return string(rt)
}

// rankingResponseKey é a chave dentro de pageProps onde está o ranking.
func rankingResponseKey(rt domain.RankingType) string {
	switch rt {
	case domain.RankingLeague:
		return "league_point_ranking"
	case domain.RankingArcade:
		return "arcade_score_ranking"
	case domain.RankingKudos:
		return "point_ranking"
	case domain.RankingMaster:
		return "master_rating_ranking"
	}
	return string(rt) + "_ranking"
}

// sf6RankingResponse é genérico — usa map pra acomodar nomes de chave variáveis.
type sf6RankingResponse struct {
	PageProps map[string]json.RawMessage `json:"pageProps"`
}

type sf6RankingBlock struct {
	CurrentPage        int                       `json:"current_page"`
	TotalPage          int                       `json:"total_page"`
	TotalCount         int                       `json:"total_count"`
	RankingFighterList []json.RawMessage         `json:"ranking_fighter_list"`
}

type sf6RankingFighter struct {
	CharacterID         int             `json:"character_id"`
	CharacterToolName   string          `json:"character_tool_name"`
	CharacterName       string          `json:"character_name"`
	LeaguePoint         int             `json:"league_point"`
	LeagueRank          int             `json:"league_rank"`
	MasterLeague        int             `json:"master_league"`
	MasterRating        int             `json:"master_rating"`
	MasterRatingRanking int             `json:"master_rating_ranking"`
	Order               int             `json:"order"`
	FighterBannerInfo   json.RawMessage `json:"fighter_banner_info"`
}

type sf6FighterBanner struct {
	HomeID       int `json:"home_id"`
	PersonalInfo struct {
		FighterID  string `json:"fighter_id"`
		ShortID    int64  `json:"short_id"`
		PlatformID int    `json:"platform_id"`
	} `json:"personal_info"`
}

// FetchRankingPage busca uma página do ranking global do SF6.
// Implementa domain/ranking.SF6Client.
func (c *Client) FetchRankingPage(ctx context.Context, rt domain.RankingType, page int) (*domain.Page, error) {
	buildID, err := c.buildID.Get(ctx)
	if err != nil {
		return nil, err
	}

	pg, status, err := c.doFetchRanking(ctx, buildID, rt, page)
	if err != nil {
		return nil, err
	}

	if status == http.StatusNotFound {
		// buildID expirou — refresh e retry
		buildID, err = c.buildID.Refresh(ctx)
		if err != nil {
			return nil, err
		}
		pg, status, err = c.doFetchRanking(ctx, buildID, rt, page)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("ranking %s page %d: %d after buildId refresh", rt, page, status)
		}
	} else if status != http.StatusOK {
		return nil, fmt.Errorf("ranking %s page %d: %d", rt, page, status)
	}

	return pg, nil
}

func (c *Client) doFetchRanking(ctx context.Context, buildID string, rt domain.RankingType, page int) (*domain.Page, int, error) {
	url := fmt.Sprintf("%s/_next/data/%s/pt-br/ranking/%s.json?page=%d",
		sf6Base, buildID, rankingPath(rt), page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("Cookie", os.Getenv("SF6_COOKIE"))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	var raw sf6RankingResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("ranking %s decode: %w", rt, err)
	}

	blockKey := rankingResponseKey(rt)
	blockRaw, ok := raw.PageProps[blockKey]
	if !ok {
		return nil, resp.StatusCode, fmt.Errorf("ranking %s: chave %q ausente em pageProps", rt, blockKey)
	}

	var block sf6RankingBlock
	if err := json.Unmarshal(blockRaw, &block); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("ranking %s block decode: %w", rt, err)
	}

	entries := make([]domain.Entry, 0, len(block.RankingFighterList))
	for _, rawEntry := range block.RankingFighterList {
		var f sf6RankingFighter
		if err := json.Unmarshal(rawEntry, &f); err != nil {
			continue // ignora entrada quebrada, segue
		}
		var banner sf6FighterBanner
		_ = json.Unmarshal(f.FighterBannerInfo, &banner) // banner é opcional

		entries = append(entries, domain.Entry{
			RankingType:       rt,
			OrderNo:           f.Order,
			ShortID:           banner.PersonalInfo.ShortID,
			FighterID:         banner.PersonalInfo.FighterID,
			CharacterID:       f.CharacterID,
			CharacterToolName: f.CharacterToolName,
			CharacterName:     f.CharacterName,
			LeaguePoint:       f.LeaguePoint,
			LeagueRank:        f.LeagueRank,
			MasterLeague:      f.MasterLeague,
			MasterRating:      f.MasterRating,
			MasterRatingOrder: f.MasterRatingRanking,
			HomeID:            banner.HomeID,
			PlatformID:        banner.PersonalInfo.PlatformID,
			FullData:          rawEntry,
		})
	}

	return &domain.Page{
		CurrentPage: block.CurrentPage,
		TotalPages:  block.TotalPage,
		TotalCount:  block.TotalCount,
		Entries:     entries,
	}, resp.StatusCode, nil
}
