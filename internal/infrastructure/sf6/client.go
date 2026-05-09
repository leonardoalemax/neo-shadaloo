package sf6

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	domain "neo-shadaloo/internal/domain/battlelog"
	domainconfig "neo-shadaloo/internal/domain/config"
)

const sf6Base = "https://www.streetfighter.com/6/buckler"

// sf6APIResponse mirrors the JSON shape returned by the SF6 Buckler Next.js API.
type sf6APIResponse struct {
	PageProps struct {
		ReplayList        []domain.Replay          `json:"replay_list"`
		CurrentPage       int                      `json:"current_page"`
		TotalPage         int                      `json:"total_page"`
		FighterBannerInfo *domain.FighterBannerInfo `json:"fighter_banner_info"`
	} `json:"pageProps"`
}

var defaultHeaders = map[string]string{
	"accept":          "*/*",
	"accept-language": "en-US,en;q=0.9",
	"referer":         sf6Base + "/",
	"sec-fetch-dest":  "empty",
	"sec-fetch-mode":  "cors",
	"sec-fetch-site":  "same-origin",
	"x-nextjs-data":   "1",
}

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"

type Client struct {
	buildID *buildIDManager
	http    *http.Client
}

func NewClient(cfgRepo domainconfig.ConfigRepository) domain.SF6Client {
	return &Client{
		buildID: newBuildIDManager(cfgRepo, userAgent),
		http:    http.DefaultClient,
	}
}

func (c *Client) FetchPage(ctx context.Context, userID string, page int) (*domain.SF6Page, error) {
	buildID, err := c.buildID.Get(ctx)
	if err != nil {
		return nil, err
	}

	result, status, err := c.doFetch(ctx, buildID, userID, page)
	if err != nil {
		return nil, err
	}

	if status == http.StatusNotFound {
		buildID, err = c.buildID.Refresh(ctx)
		if err != nil {
			return nil, err
		}
		result, status, err = c.doFetch(ctx, buildID, userID, page)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("SF6 page %d returned %d after buildId refresh", page, status)
		}
	} else if status != http.StatusOK {
		return nil, fmt.Errorf("SF6 page %d returned %d", page, status)
	}

	return &domain.SF6Page{
		Replays:    result.PageProps.ReplayList,
		TotalPages: result.PageProps.TotalPage,
		BannerInfo: result.PageProps.FighterBannerInfo,
	}, nil
}

func (c *Client) doFetch(ctx context.Context, buildID, userID string, page int) (*sf6APIResponse, int, error) {
	url := fmt.Sprintf("%s/_next/data/%s/en/profile/%s/battlelog.json?page=%d&sid=%s",
		sf6Base, buildID, userID, page, userID)

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

	var result sf6APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, resp.StatusCode, err
	}
	return &result, resp.StatusCode, nil
}
