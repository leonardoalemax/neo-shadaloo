package battlelog

import "context"

// SF6Page represents a single page of results from the SF6 Buckler API.
type SF6Page struct {
	Replays    []Replay
	TotalPages int
	BannerInfo *FighterBannerInfo
}

// SF6Client is the port through which the domain accesses the external SF6 API.
// The concrete implementation lives in infrastructure/sf6.
type SF6Client interface {
	FetchPage(ctx context.Context, userID string, page int) (*SF6Page, error)
}
