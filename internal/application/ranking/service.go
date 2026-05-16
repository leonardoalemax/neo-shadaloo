package ranking

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	battlelog "neo-shadaloo/internal/domain/battlelog"
	domain "neo-shadaloo/internal/domain/ranking"
	kafkainfra "neo-shadaloo/internal/infrastructure/kafka"
)

const (
	defaultBatchSize = 200             // entries por insert no banco
	workerCount      = 5               // consumers paralelos
	retryPause       = 5 * time.Minute // pausa quando toma 405
)

// Service orquestra o sync do ranking global via Kafka.
type Service struct {
	repo        domain.Repository
	sf6         domain.SF6Client
	playerIndex battlelog.PlayerIndexRepository

	writer *kafkago.Writer

	// Garante que só um sync por ranking_type rode por vez.
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

// InitKafka inicializa o writer Kafka e inicia os consumers.
// Deve ser chamado no main.go após criar o Service.
func (s *Service) InitKafka(ctx context.Context) error {
	if err := kafkainfra.EnsureTopic(ctx); err != nil {
		log.Printf("[ranking] kafka: falha ao garantir topic (pode já existir): %v", err)
	}

	s.writer = kafkainfra.NewWriter()

	// Inicia workers consumers
	for i := 0; i < workerCount; i++ {
		go s.consumeLoop(ctx, i)
	}
	log.Printf("[ranking] kafka: %d consumers iniciados", workerCount)
	return nil
}

// TriggerSyncAll dispara sync de todos os 4 rankings.
func (s *Service) TriggerSyncAll() {
	for _, rt := range domain.AllRankingTypes() {
		s.TriggerSync(rt)
	}
}

// TriggerSync descobre total_pages e enfileira cada página no Kafka.
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
		if err := s.produceSync(context.Background(), rt); err != nil {
			log.Printf("[ranking] %s: falha ao produzir páginas no kafka: %v", rt, err)
			s.mu.Lock()
			delete(s.running, rt)
			s.mu.Unlock()
		}
	}()
}

// produceSync busca página 1 pra descobrir totalPages, limpa dados antigos
// e enfileira todas as páginas restantes no Kafka.
func (s *Service) produceSync(ctx context.Context, rt domain.RankingType) error {
	start := time.Now()
	startedAt := start.Unix()
	log.Printf("[ranking] %s: iniciando sync (producing pages)", rt)

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

	// 4) Produz páginas 2..totalPages no Kafka
	if totalPages > 1 {
		if err := kafkainfra.ProducePages(ctx, s.writer, string(rt), 2, totalPages, totalCount, startedAt); err != nil {
			return err
		}
		log.Printf("[ranking] %s: %d páginas enfileiradas no kafka", rt, totalPages-1)
	}

	return nil
}

// consumeLoop é o loop de um consumer worker.
// Consome mensagens do Kafka, busca a página no SF6, persiste.
// Em caso de 405, pausa por 5 minutos antes de continuar.
func (s *Service) consumeLoop(ctx context.Context, workerID int) {
	reader := kafkainfra.NewReader("ranking-sync-consumers")
	defer reader.Close()

	log.Printf("[ranking] kafka worker=%d: consumer iniciado", workerID)

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[ranking] kafka worker=%d: fetch erro: %v", workerID, err)
			time.Sleep(time.Second)
			continue
		}

		var pm kafkainfra.PageMessage
		if err := json.Unmarshal(msg.Value, &pm); err != nil {
			log.Printf("[ranking] kafka worker=%d: decode erro: %v", workerID, err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		rt := domain.RankingType(pm.RankingType)

		// Busca a página no SF6
		reqStart := time.Now()
		pg, fetchErr := s.sf6.FetchRankingPage(ctx, rt, pm.Page)

		if fetchErr != nil {
			errStr := fetchErr.Error()
			// Detecta 405 — rate limit
			if strings.Contains(errStr, "405") {
				log.Printf("[ranking] %s: pg=%d worker=%d GOT 405 — pausando %s",
					rt, pm.Page, workerID, retryPause)

				// NÃO commita a mensagem — vai ser re-processada depois da pausa
				time.Sleep(retryPause)
				continue
			}
			// Outro erro — loga e commita (perde a página, melhor que ficar em loop)
			log.Printf("[ranking] %s: pg=%d worker=%d erro: %v (skipping)", rt, pm.Page, workerID, fetchErr)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		log.Printf("[ranking] %s: pg=%d/%d worker=%d entries=%d took=%s",
			rt, pm.Page, pm.TotalPages, workerID, len(pg.Entries), time.Since(reqStart).Round(time.Millisecond))

		// Persiste
		if err := s.repo.AppendBatch(ctx, pg.Entries); err != nil {
			log.Printf("[ranking] %s: pg=%d worker=%d persist erro: %v", rt, pm.Page, workerID, err)
		}
		s.indexPlayers(ctx, pg.Entries)

		// Commita a mensagem
		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("[ranking] kafka worker=%d: commit erro: %v", workerID, err)
		}

		// Atualiza meta periodicamente
		s.updateProgress(ctx, rt, pm)
	}
}

// progressTickers conta msgs processadas localmente só pra decidir QUANDO
// atualizar meta (a cada 50 msgs). O VALOR salvo vem do banco.
var progressTickers sync.Map // map[RankingType]*atomic.Int64

func (s *Service) updateProgress(ctx context.Context, rt domain.RankingType, pm kafkainfra.PageMessage) {
	key := string(rt)
	val, _ := progressTickers.LoadOrStore(key, &atomic.Int64{})
	tick := val.(*atomic.Int64).Add(1)

	// Atualiza meta a cada 50 msgs processadas
	if tick%50 != 0 {
		return
	}

	synced, err := s.repo.CountSyncedPages(ctx, rt)
	if err != nil {
		log.Printf("[ranking] %s: count synced pages erro: %v", rt, err)
		return
	}

	status := "running"
	finishedAt := int64(0)
	complete := synced >= pm.TotalPages
	if complete {
		status = "done"
		finishedAt = time.Now().Unix()
	}

	_ = s.repo.SaveMeta(ctx, domain.SnapshotMeta{
		RankingType:  rt,
		TotalCount:   pm.TotalCount,
		TotalPages:   pm.TotalPages,
		SyncedPages:  synced,
		UpdatedAt:    time.Now().Unix(),
		StartedAt:    pm.StartedAt,
		LastSyncedAt: finishedAt,
		Status:       status,
	})

	log.Printf("[ranking] %s: progresso %d/%d páginas (%.1f%%)",
		rt, synced, pm.TotalPages, float64(synced)/float64(pm.TotalPages)*100)

	if complete {
		log.Printf("[ranking] %s: sync COMPLETO", rt)
		progressTickers.Delete(key)
		s.mu.Lock()
		delete(s.running, rt)
		s.mu.Unlock()
	}
}

// IsRunning indica se um sync está em curso pra um ranking_type.
func (s *Service) IsRunning(rt domain.RankingType) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[rt]
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
		if err := s.playerIndex.UpsertPreserveCharacter(context.Background(), p); err != nil {
			log.Printf("[ranking] player_index upsert falhou (%d players): %v", len(p), err)
		}
	}(players)
}
