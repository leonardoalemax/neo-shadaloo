package league

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	battlelog "neo-shadaloo/internal/domain/battlelog"
	domain "neo-shadaloo/internal/domain/league"
	rankingdomain "neo-shadaloo/internal/domain/ranking"
)

const (
	// Concorrência máxima de requests simultâneos.
	maxConcurrency = 10
	// Rate limit: requests por segundo (evita 405).
	reqPerSecond = 5
	// Pausa ao tomar 405 (backoff antes de retomar).
	backoff405 = 30 * time.Second
	// Máximo de retries por página antes de pular.
	maxRetries = 5
)

// Service sincroniza o ranking league_point usando pool de goroutines + rate limiter.
type Service struct {
	repo        domain.Repository
	sf6         rankingdomain.SF6Client
	playerIndex battlelog.PlayerIndexRepository
	limiter     *rate.Limiter

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

func NewService(repo domain.Repository, sf6 rankingdomain.SF6Client, playerIndex battlelog.PlayerIndexRepository) *Service {
	return &Service{
		repo:        repo,
		sf6:         sf6,
		playerIndex: playerIndex,
		limiter:     rate.NewLimiter(rate.Limit(reqPerSecond), maxConcurrency),
	}
}

// TriggerSync dispara o sync em background. Se já estiver rodando, ignora.
func (s *Service) TriggerSync() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Printf("[league] sync já em curso, ignorando")
		return
	}
	s.running = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		s.runSync(ctx)
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.mu.Unlock()
	}()
}

// StopSync cancela o sync em curso.
func (s *Service) StopSync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		log.Printf("[league] sync cancelado pelo usuário")
	}
}

func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Service) runSync(ctx context.Context) {
	startedAt := time.Now().Unix()
	log.Printf("[league] iniciando sync")

	// 1. Busca página 1 pra saber totalPages
	if err := s.limiter.Wait(ctx); err != nil {
		return
	}
	first, err := s.sf6.FetchRankingPage(ctx, rankingdomain.RankingLeague, 1)
	if err != nil {
		log.Printf("[league] erro ao buscar página 1: %v", err)
		_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
			UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "failed",
		})
		return
	}

	totalPages := first.TotalPages
	totalCount := first.TotalCount
	log.Printf("[league] total_pages=%d total_count=%d", totalPages, totalCount)

	// 2. Verifica se há sync anterior pra resume
	meta, _ := s.repo.GetMeta(ctx)
	resumeFrom := 1
	if meta != nil && meta.Status == "running" && meta.SyncedPages > 0 && meta.TotalPages == totalPages {
		resumeFrom = meta.SyncedPages + 1
		log.Printf("[league] resumindo do sync anterior — página %d/%d", resumeFrom, totalPages)
	}

	_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
		TotalCount: totalCount, TotalPages: totalPages, SyncedPages: resumeFrom - 1,
		UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "running",
	})

	// Persiste página 1 (se não é resume ou resume a partir de 1)
	if resumeFrom <= 1 {
		s.persistPage(ctx, first.Entries)
		resumeFrom = 2
	}

	if totalPages < 2 {
		s.finishSync(ctx, totalCount, totalPages, startedAt)
		return
	}

	// 3. Pool de goroutines com semáforo
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var syncedPages int64
	var syncedMu sync.Mutex

	// Canal pra páginas
	pages := make(chan int, maxConcurrency*2)

	// Producer: enfileira páginas
	go func() {
		defer close(pages)
		for p := resumeFrom; p <= totalPages; p++ {
			select {
			case pages <- p:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Workers
	for page := range pages {
		if ctx.Err() != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(pg int) {
			defer wg.Done()
			defer func() { <-sem }()

			s.fetchAndPersist(ctx, pg, totalPages)

			syncedMu.Lock()
			syncedPages++
			done := int(syncedPages)
			syncedMu.Unlock()

			// Atualiza progresso a cada 50 páginas
			if done%50 == 0 || pg == totalPages {
				_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
					TotalCount:  totalCount,
					TotalPages:  totalPages,
					SyncedPages: (resumeFrom - 1) + done,
					UpdatedAt:   time.Now().Unix(),
					StartedAt:   startedAt,
					Status:      "running",
				})
				log.Printf("[league] progresso %d/%d páginas (%.1f%%)",
					(resumeFrom-1)+done, totalPages,
					float64((resumeFrom-1)+done)/float64(totalPages)*100)
			}
		}(page)
	}

	wg.Wait()

	if ctx.Err() != nil {
		log.Printf("[league] sync cancelado")
		return
	}

	s.finishSync(ctx, totalCount, totalPages, startedAt)
}

func (s *Service) fetchAndPersist(ctx context.Context, page, totalPages int) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			return
		}

		// Rate limiter — espera slot disponível
		if err := s.limiter.Wait(ctx); err != nil {
			return
		}

		reqStart := time.Now()
		pg, err := s.sf6.FetchRankingPage(ctx, rankingdomain.RankingLeague, page)
		if err != nil {
			if strings.Contains(err.Error(), "405") {
				log.Printf("[league] pg=%d GOT 405 — backoff %s (attempt %d/%d)",
					page, backoff405, attempt+1, maxRetries)
				select {
				case <-time.After(backoff405):
				case <-ctx.Done():
					return
				}
				continue
			}
			log.Printf("[league] pg=%d erro: %v (attempt %d/%d)", page, err, attempt+1, maxRetries)
			time.Sleep(2 * time.Second)
			continue
		}

		log.Printf("[league] pg=%d/%d entries=%d took=%s",
			page, totalPages, len(pg.Entries), time.Since(reqStart).Round(time.Millisecond))

		s.persistPage(ctx, pg.Entries)
		return
	}

	log.Printf("[league] pg=%d DESISTINDO após %d tentativas", page, maxRetries)
}

func (s *Service) finishSync(ctx context.Context, totalCount, totalPages int, startedAt int64) {
	_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
		TotalCount:   totalCount,
		TotalPages:   totalPages,
		SyncedPages:  totalPages,
		UpdatedAt:    time.Now().Unix(),
		StartedAt:    startedAt,
		LastSyncedAt: time.Now().Unix(),
		Status:       "done",
	})
	log.Printf("[league] sync COMPLETO — %d páginas", totalPages)
}

// persistPage converte entries → players e upserta + indexa player_index.
func (s *Service) persistPage(ctx context.Context, entries []rankingdomain.Entry) {
	if len(entries) == 0 {
		return
	}
	now := time.Now().Unix()
	players := make([]domain.Player, 0, len(entries))
	for _, e := range entries {
		if e.ShortID == 0 {
			continue
		}
		players = append(players, domain.Player{
			ShortID:           e.ShortID,
			FighterID:         e.FighterID,
			CharacterID:       e.CharacterID,
			CharacterToolName: e.CharacterToolName,
			CharacterName:     e.CharacterName,
			LeaguePoint:       e.LeaguePoint,
			LeagueRank:        e.LeagueRank,
			MasterLeague:      e.MasterLeague,
			MasterRating:      e.MasterRating,
			HomeID:            e.HomeID,
			PlatformID:        e.PlatformID,
			OrderNo:           e.OrderNo,
			FullData:          e.FullData,
			UpdatedAt:         now,
		})
	}
	if err := s.repo.UpsertBatch(ctx, players); err != nil {
		log.Printf("[league] upsert erro: %v", err)
	}
	s.indexPlayers(ctx, players)
}

func (s *Service) indexPlayers(ctx context.Context, players []domain.Player) {
	if s.playerIndex == nil || len(players) == 0 {
		return
	}
	now := time.Now().Unix()
	out := make([]battlelog.PlayerEntry, 0, len(players))
	for _, p := range players {
		if p.FighterID == "" {
			continue
		}
		out = append(out, battlelog.PlayerEntry{
			FighterID:         p.FighterID,
			ShortID:           p.ShortID,
			CharacterToolName: p.CharacterToolName,
			UpdatedAt:         now,
		})
	}
	if len(out) == 0 {
		return
	}
	go func(p []battlelog.PlayerEntry) {
		if err := s.playerIndex.UpsertPreserveCharacter(context.Background(), p); err != nil {
			log.Printf("[league] player_index upsert falhou (%d players): %v", len(p), err)
		}
	}(out)
}

// ── Read methods (pra handlers) ──────────────────────────────────────────────

func (s *Service) GetMeta(ctx context.Context) (*domain.SyncMeta, error) {
	return s.repo.GetMeta(ctx)
}

func (s *Service) PlayersByCountry(ctx context.Context, f domain.MapFilter) ([]domain.CountryPlayerCount, error) {
	return s.repo.PlayersByCountry(ctx, f)
}

func (s *Service) DistinctCharacters(ctx context.Context) ([]domain.CharacterCount, error) {
	return s.repo.DistinctCharacters(ctx)
}

func (s *Service) DistinctRanks(ctx context.Context) ([]domain.RankCount, error) {
	return s.repo.DistinctRanks(ctx)
}
