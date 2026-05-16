package ranking

import (
	"context"
	"log"
	"sync"
	"time"

	battlelog "neo-shadaloo/internal/domain/battlelog"
	domain "neo-shadaloo/internal/domain/ranking"
)

// Configuração do crawl. Valores conservadores pra não tomar 429 do SF6.
const (
	defaultConcurrency = 10                     // workers paralelos chamando o SF6
	defaultBatchSize   = 200                    // entries por insert no banco
	requestPause       = 200 * time.Millisecond // pausa entre requests por worker
	progressLogEvery   = 500                    // loga a cada N páginas
)

// Service orquestra o sync do ranking global.
type Service struct {
	repo        domain.Repository
	sf6         domain.SF6Client
	playerIndex battlelog.PlayerIndexRepository // ← também indexa os players encontrados

	// Garante que só um sync por ranking_type rode por vez (proteção em memória).
	mu      sync.Mutex
	running map[domain.RankingType]bool
}

func NewService(repo domain.Repository, sf6 domain.SF6Client, playerIndex battlelog.PlayerIndexRepository) *Service {
	return &Service{
		repo:        repo,
		sf6:         sf6,
		playerIndex: playerIndex,
		running:     make(map[domain.RankingType]bool),
	}
}

// TriggerSyncAll dispara sync de todos os 4 rankings em background (fire-and-forget).
func (s *Service) TriggerSyncAll() {
	for _, rt := range domain.AllRankingTypes() {
		s.TriggerSync(rt)
	}
}

// TriggerSync dispara o sync de um ranking específico em background.
// Se já houver sync em curso pra esse tipo, ignora.
func (s *Service) TriggerSync(rt domain.RankingType) {
	s.mu.Lock()
	if s.running[rt] {
		s.mu.Unlock()
		log.Printf("[ranking] %s: sync já em curso, ignorando trigger", rt)
		return
	}
	s.running[rt] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, rt)
			s.mu.Unlock()
		}()
		if err := s.runSync(context.Background(), rt); err != nil {
			log.Printf("[ranking] %s: sync falhou: %v", rt, err)
		}
	}()
}

// IsRunning indica se um sync está em curso pra um ranking_type.
func (s *Service) IsRunning(rt domain.RankingType) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[rt]
}

// runSync executa o crawl completo de um ranking_type.
func (s *Service) runSync(ctx context.Context, rt domain.RankingType) error {
	start := time.Now()
	startedAt := start.Unix()
	log.Printf("[ranking] %s: iniciando sync", rt)

	// 1) Página 1 pra descobrir total_pages
	first, err := s.sf6.FetchRankingPage(ctx, rt, 1)
	if err != nil {
		_ = s.repo.SaveMeta(ctx, domain.SnapshotMeta{
			RankingType: rt, UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "failed",
		})
		return err
	}
	totalPages := first.TotalPages
	totalCount := first.TotalCount
	log.Printf("[ranking] %s: total_pages=%d total_count=%d", rt, totalPages, totalCount)

	// 2) Limpa snapshot anterior
	if err := s.repo.ClearType(ctx, rt); err != nil {
		return err
	}
	_ = s.repo.SaveMeta(ctx, domain.SnapshotMeta{
		RankingType: rt, TotalCount: totalCount, TotalPages: totalPages,
		SyncedPages: 0, UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "running",
	})

	// 3) Persiste página 1 (já buscada) + indexa players
	if err := s.repo.AppendBatch(ctx, first.Entries); err != nil {
		return err
	}
	s.indexPlayers(ctx, first.Entries)

	// 4) Worker pool pra páginas 2..totalPages
	pageCh := make(chan int, defaultConcurrency*2)
	entriesCh := make(chan []domain.Entry, defaultConcurrency*2)
	var wgFetch sync.WaitGroup

	for i := 0; i < defaultConcurrency; i++ {
		wgFetch.Add(1)
		go func(workerID int) {
			defer wgFetch.Done()
			for page := range pageCh {
				time.Sleep(requestPause)
				reqStart := time.Now()
				pg, err := s.sf6.FetchRankingPage(ctx, rt, page)
				if err != nil {
					log.Printf("[ranking] %s: pg=%d worker=%d erro: %v", rt, page, workerID, err)
					continue
				}
				log.Printf("[ranking] %s: pg=%d/%d worker=%d entries=%d took=%s",
					rt, page, totalPages, workerID, len(pg.Entries), time.Since(reqStart).Round(time.Millisecond))
				entriesCh <- pg.Entries
			}
		}(i)
	}

	// Goroutine que fecha entriesCh quando todos workers terminarem
	go func() {
		wgFetch.Wait()
		close(entriesCh)
	}()

	// Goroutine que enfileira páginas
	go func() {
		for page := 2; page <= totalPages; page++ {
			pageCh <- page
		}
		close(pageCh)
	}()

	// 5) Consumidor: junta entries em batches e persiste
	batch := make([]domain.Entry, 0, defaultBatchSize)
	syncedPages := 1 // já fez a página 1
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.repo.AppendBatch(ctx, batch); err != nil {
			return err
		}
		// indexa players em paralelo ao próximo fetch (não bloqueia)
		s.indexPlayers(ctx, batch)
		batch = batch[:0]
		return nil
	}

	for entries := range entriesCh {
		batch = append(batch, entries...)
		syncedPages++
		if len(batch) >= defaultBatchSize {
			if err := flush(); err != nil {
				log.Printf("[ranking] %s: flush erro: %v", rt, err)
			}
		}
		if syncedPages%progressLogEvery == 0 {
			log.Printf("[ranking] %s: progresso %d/%d páginas (%.1f%%)",
				rt, syncedPages, totalPages, float64(syncedPages)/float64(totalPages)*100)
			_ = s.repo.SaveMeta(ctx, domain.SnapshotMeta{
				RankingType: rt, TotalCount: totalCount, TotalPages: totalPages,
				SyncedPages: syncedPages, UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "running",
			})
		}
	}
	_ = flush()

	dur := time.Since(start)
	finishedAt := time.Now().Unix()
	log.Printf("[ranking] %s: sync concluído em %s (%d/%d páginas)", rt, dur, syncedPages, totalPages)
	_ = s.repo.SaveMeta(ctx, domain.SnapshotMeta{
		RankingType: rt, TotalCount: totalCount, TotalPages: totalPages,
		SyncedPages: syncedPages, UpdatedAt: finishedAt, StartedAt: startedAt, LastSyncedAt: finishedAt, Status: "done",
	})
	return nil
}

// GetMeta retorna o estado de sync atual de um ranking.
func (s *Service) GetMeta(ctx context.Context, rt domain.RankingType) (*domain.SnapshotMeta, error) {
	return s.repo.GetMeta(ctx, rt)
}

// List delega pra repo (paginação + filtros).
func (s *Service) List(ctx context.Context, rt domain.RankingType, f domain.ListFilter) (*domain.ListPage, error) {
	return s.repo.List(ctx, rt, f)
}

// ByPlayer devolve entries de um jogador específico no ranking.
func (s *Service) ByPlayer(ctx context.Context, rt domain.RankingType, shortID int64) ([]domain.Entry, error) {
	return s.repo.ByPlayer(ctx, rt, shortID)
}

// Around devolve vizinhança em torno de uma posição.
func (s *Service) Around(ctx context.Context, rt domain.RankingType, order, radius int) ([]domain.Entry, error) {
	return s.repo.Around(ctx, rt, order, radius)
}

// Facets devolve contadores pra preencher filtros.
func (s *Service) Facets(ctx context.Context, rt domain.RankingType) (*domain.Facets, error) {
	return s.repo.FacetsOf(ctx, rt)
}

// PlayersByCountry devolve players únicos por país pra renderizar o mapa.
func (s *Service) PlayersByCountry(ctx context.Context, rt domain.RankingType) ([]domain.CountryPlayerCount, error) {
	return s.repo.PlayersByCountry(ctx, rt)
}

// indexPlayers extrai os players únicos do batch (dedup por short_id) e
// faz upsert no player_index. Roda em goroutine pra não bloquear o crawl.
// Erros são apenas logados — falha no índice não pode interromper o sync.
func (s *Service) indexPlayers(ctx context.Context, entries []domain.Entry) {
	if s.playerIndex == nil || len(entries) == 0 {
		return
	}

	now := time.Now().Unix()
	seen := make(map[int64]bool, len(entries))
	players := make([]battlelog.PlayerEntry, 0, len(entries))
	for _, e := range entries {
		if e.ShortID == 0 || e.FighterID == "" {
			continue
		}
		if seen[e.ShortID] {
			continue
		}
		seen[e.ShortID] = true
		players = append(players, battlelog.PlayerEntry{
			FighterID:         e.FighterID,
			ShortID:           e.ShortID,
			CharacterToolName: e.CharacterToolName,
			UpdatedAt:         now,
		})
	}

	if len(players) == 0 {
		return
	}

	go func(p []battlelog.PlayerEntry) {
		// Usa UpsertPreserveCharacter — players novos entram com o character do
		// ranking, mas players já existentes mantêm o character original
		// (provavelmente vindo do battlelog sync que é mais autoritativo).
		if err := s.playerIndex.UpsertPreserveCharacter(context.Background(), p); err != nil {
			log.Printf("[ranking] player_index upsert falhou (%d players): %v", len(p), err)
		}
	}(players)
}
