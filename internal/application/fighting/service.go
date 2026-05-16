package fighting

import (
	"context"
	"log"
	"time"

	domain "neo-shadaloo/internal/domain/fighting"
)

const cacheTTL = 24 * int64(time.Hour/time.Millisecond)

type FightingService struct {
	repo   domain.FightingRepository
	client domain.FightingClient
}

func NewFightingService(repo domain.FightingRepository, client domain.FightingClient) *FightingService {
	return &FightingService{repo: repo, client: client}
}

func (s *FightingService) GetAvailableMonths(ctx context.Context) ([]string, error) {
	return s.repo.ListMonths(ctx)
}

func (s *FightingService) GetFighting(ctx context.Context, yyyymm string) (*domain.FightingSnapshot, error) {
	snap, err := s.repo.Get(ctx, yyyymm)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		s.TriggerSync(yyyymm)
		return &domain.FightingSnapshot{YYYYMM: yyyymm, Leagues: []domain.LeagueFighting{}}, nil
	}
	if time.Now().UnixMilli()-snap.CachedAt > cacheTTL {
		s.TriggerSync(yyyymm)
	}
	return snap, nil
}

func (s *FightingService) TriggerSync(yyyymm string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.sync(ctx, yyyymm); err != nil {
			log.Printf("[fighting-sync] %s error: %v", yyyymm, err)
		}
	}()
}

func (s *FightingService) ForceSync(yyyymm string) {
	s.TriggerSync(yyyymm)
}

func (s *FightingService) sync(ctx context.Context, yyyymm string) error {
	leagues, err := s.client.FetchFighting(ctx, yyyymm)
	if err != nil {
		return err
	}
	snap := &domain.FightingSnapshot{
		YYYYMM:   yyyymm,
		Leagues:  leagues,
		CachedAt: time.Now().UnixMilli(),
	}
	return s.repo.Save(ctx, snap)
}
