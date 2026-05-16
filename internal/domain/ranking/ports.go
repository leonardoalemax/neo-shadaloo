package ranking

import "context"

// Page representa uma página de ranking retornada pelo SF6.
type Page struct {
	CurrentPage int
	TotalPages  int
	TotalCount  int
	Entries     []Entry
}

// SF6Client abstrai o fetch de uma página de ranking do SF6.
type SF6Client interface {
	FetchRankingPage(ctx context.Context, rt RankingType, page int) (*Page, error)
}
