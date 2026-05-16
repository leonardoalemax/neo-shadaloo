package usage

import (
	"context"
	"log"
	"sync"
	"time"

	domain "neo-shadaloo/internal/domain/usage"
)

const cacheTTL = 24 * 60 * 60 * 1000 // 24 hours in milliseconds

type UsageService struct {
	repo   domain.UsageRepository
	client domain.UsageClient

	mu      sync.Mutex
	syncing map[string]bool
}

func NewUsageService(repo domain.UsageRepository, client domain.UsageClient) *UsageService {
	return &UsageService{
		repo:    repo,
		client:  client,
		syncing: make(map[string]bool),
	}
}

// GetUsage returns the cached usage snapshot for the given YYYYMM.
// If stale or missing, a background sync is triggered.
func (s *UsageService) GetUsage(ctx context.Context, yyyymm string) (*domain.UsageSnapshot, error) {
	snap, err := s.repo.Get(ctx, yyyymm)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		s.TriggerSync(yyyymm)
		return &domain.UsageSnapshot{YYYYMM: yyyymm, Leagues: []domain.LeagueUsage{}}, nil
	}
	if time.Now().UnixMilli()-snap.CachedAt > cacheTTL {
		s.TriggerSync(yyyymm)
	}
	return snap, nil
}

// GetAvailableMonths returns all YYYYMM periods that have cached data, sorted newest-first.
func (s *UsageService) GetAvailableMonths(ctx context.Context) ([]string, error) {
	return s.repo.ListMonths(ctx)
}

// TriggerSync starts a background sync for yyyymm only if none is already running.
func (s *UsageService) TriggerSync(yyyymm string) {
	s.mu.Lock()
	if s.syncing[yyyymm] {
		s.mu.Unlock()
		return
	}
	s.syncing[yyyymm] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.syncing, yyyymm)
			s.mu.Unlock()
		}()
		s.runSync(yyyymm)
	}()
}

// ForceSync triggers an immediate sync regardless of whether one is running.
func (s *UsageService) ForceSync(yyyymm string) {
	go s.runSync(yyyymm)
}

func (s *UsageService) runSync(yyyymm string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("[usage-sync] Starting sync for %s", yyyymm)

	leagues, err := s.client.FetchUsage(ctx, yyyymm)
	if err != nil {
		log.Printf("[usage-sync] FetchUsage error for %s: %v", yyyymm, err)
		return
	}

	snap := &domain.UsageSnapshot{
		YYYYMM:   yyyymm,
		Leagues:  leagues,
		CachedAt: time.Now().UnixMilli(),
	}
	if err := s.repo.Save(ctx, snap); err != nil {
		log.Printf("[usage-sync] Save error for %s: %v", yyyymm, err)
		return
	}

	log.Printf("[usage-sync] Done %s: %d leagues", yyyymm, len(leagues))
}
