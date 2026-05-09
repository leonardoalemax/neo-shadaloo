package battlelog

import (
	"context"
	"log"
	"sync"
	"time"

	domain "neo-shadaloo/internal/domain/battlelog"
)

// BattlelogService orchestrates battlelog use cases.
type BattlelogService struct {
	repo      domain.BattlelogRepository
	sf6       domain.SF6Client
	publisher domain.EventPublisher

	mu      sync.Mutex
	syncing map[string]bool
}

func NewBattlelogService(
	repo domain.BattlelogRepository,
	sf6 domain.SF6Client,
	publisher domain.EventPublisher,
) *BattlelogService {
	return &BattlelogService{
		repo:      repo,
		sf6:       sf6,
		publisher: publisher,
		syncing:   make(map[string]bool),
	}
}

// GetBattlelog returns the cached battlelog from the repository.
// If the cache is stale, a background sync is triggered without blocking.
// If no data exists yet, an empty battlelog is returned and sync is triggered.
func (s *BattlelogService) GetBattlelog(ctx context.Context, userID string) (*domain.Battlelog, error) {
	bl, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if bl == nil {
		s.TriggerSync(userID)
		return &domain.Battlelog{UserID: userID, Replays: []domain.Replay{}}, nil
	}

	if bl.IsStale() {
		s.TriggerSync(userID)
	}

	return bl, nil
}

// TriggerSync starts a background sync for userID only if none is already running.
func (s *BattlelogService) TriggerSync(userID string) {
	s.mu.Lock()
	if s.syncing[userID] {
		s.mu.Unlock()
		return
	}
	s.syncing[userID] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.syncing, userID)
			s.mu.Unlock()
		}()
		s.runSync(userID)
	}()
}

// ForceSync triggers an immediate sync regardless of whether one is running.
func (s *BattlelogService) ForceSync(userID string) {
	go s.runSync(userID)
}

func (s *BattlelogService) runSync(userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	log.Printf("[sync] Starting sync for %s", userID)

	existing, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		log.Printf("[sync] repo.GetByUserID error for %s: %v", userID, err)
		return
	}
	if existing == nil {
		existing = &domain.Battlelog{UserID: userID}
	}

	// Fetch page 1 to detect new replays
	page1, err := s.sf6.FetchPage(ctx, userID, 1)
	if err != nil {
		log.Printf("[sync] FetchPage(1) error for %s: %v", userID, err)
		return
	}

	// Check for new replays — skip full fetch if nothing changed
	hasNew := false
	for _, r := range page1.Replays {
		if !existing.HasReplay(r.ReplayID) {
			hasNew = true
			break
		}
	}

	if !hasNew && len(existing.Replays) > 0 {
		log.Printf("[sync] No new replays for %s, refreshing timestamp", userID)
		existing.TouchCachedAt()
		if err := s.repo.Save(ctx, existing); err != nil {
			log.Printf("[sync] repo.Save error for %s: %v", userID, err)
		}
		return
	}

	// Fetch remaining pages in parallel
	allReplays := make([]domain.Replay, 0, len(page1.Replays))
	allReplays = append(allReplays, page1.Replays...)

	if page1.TotalPages > 1 {
		type result struct {
			replays []domain.Replay
			err     error
		}
		ch := make(chan result, page1.TotalPages-1)

		for p := 2; p <= page1.TotalPages; p++ {
			go func(page int) {
				r, err := s.sf6.FetchPage(ctx, userID, page)
				if err != nil {
					ch <- result{err: err}
					return
				}
				ch <- result{replays: r.Replays}
			}(p)
		}

		for i := 2; i <= page1.TotalPages; i++ {
			res := <-ch
			if res.err != nil {
				log.Printf("[sync] FetchPage error for %s: %v", userID, res.err)
				continue
			}
			allReplays = append(allReplays, res.replays...)
		}
	}

	existing.MergeWith(allReplays, page1.BannerInfo)

	if err := s.repo.Save(ctx, existing); err != nil {
		log.Printf("[sync] repo.Save error for %s: %v", userID, err)
		return
	}

	log.Printf("[sync] Sync complete for %s: %d replays", userID, len(existing.Replays))

	s.publisher.Publish(domain.BattlelogSyncedEvent{
		UserID:   userID,
		CachedAt: existing.CachedAt,
	})
}
