package league

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
	domain "neo-shadaloo/internal/domain/league"
	rankingdomain "neo-shadaloo/internal/domain/ranking"
	kafkainfra "neo-shadaloo/internal/infrastructure/kafka"
)

const (
	workerCount = 5
	retryPause  = 5 * time.Minute
)

// Service sincroniza o ranking league_point usando Kafka + tabela league_player.
type Service struct {
	repo        domain.Repository
	sf6         rankingdomain.SF6Client // reusa o client de ranking pra fetch
	playerIndex battlelog.PlayerIndexRepository

	writer *kafkago.Writer

	mu      sync.Mutex
	running bool
}

func NewService(repo domain.Repository, sf6 rankingdomain.SF6Client, playerIndex battlelog.PlayerIndexRepository) *Service {
	return &Service{
		repo:        repo,
		sf6:         sf6,
		playerIndex: playerIndex,
	}
}

// InitKafka inicializa writer + consumers.
func (s *Service) InitKafka(ctx context.Context) error {
	if err := kafkainfra.EnsureTopicNamed(ctx, kafkainfra.TopicLeagueSync, workerCount); err != nil {
		log.Printf("[league] kafka: falha ao garantir topic: %v", err)
	}
	s.writer = kafkainfra.NewWriterFor(kafkainfra.TopicLeagueSync)
	for i := 0; i < workerCount; i++ {
		go s.consumeLoop(ctx, i)
	}
	log.Printf("[league] kafka: %d consumers iniciados", workerCount)
	return nil
}

// TriggerSync busca página 1, descobre totalPages, enfileira páginas 2..N.
// NÃO apaga dados antigos — players são upserted.
func (s *Service) TriggerSync() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Printf("[league] sync já em curso, ignorando")
		return
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		if err := s.produce(context.Background()); err != nil {
			log.Printf("[league] produce falhou: %v", err)
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}
	}()
}

func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Service) produce(ctx context.Context) error {
	startedAt := time.Now().Unix()
	log.Printf("[league] iniciando sync (producing pages)")

	first, err := s.sf6.FetchRankingPage(ctx, rankingdomain.RankingLeague, 1)
	if err != nil {
		_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
			UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "failed",
		})
		return err
	}
	totalPages := first.TotalPages
	totalCount := first.TotalCount
	log.Printf("[league] total_pages=%d total_count=%d", totalPages, totalCount)

	_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
		TotalCount: totalCount, TotalPages: totalPages, SyncedPages: 0,
		UpdatedAt: time.Now().Unix(), StartedAt: startedAt, Status: "running",
	})

	// Persiste página 1
	s.persistPage(ctx, first.Entries)

	if totalPages > 1 {
		if err := kafkainfra.ProduceLeaguePages(ctx, s.writer, 2, totalPages, totalCount, startedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) consumeLoop(ctx context.Context, workerID int) {
	reader := kafkainfra.NewReaderFor(kafkainfra.TopicLeagueSync, "league-sync-consumers")
	defer reader.Close()
	log.Printf("[league] kafka worker=%d: consumer iniciado", workerID)

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[league] worker=%d fetch erro: %v", workerID, err)
			time.Sleep(time.Second)
			continue
		}

		var pm kafkainfra.LeaguePageMessage
		if err := json.Unmarshal(msg.Value, &pm); err != nil {
			log.Printf("[league] worker=%d decode erro: %v", workerID, err)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		reqStart := time.Now()
		pg, fetchErr := s.sf6.FetchRankingPage(ctx, rankingdomain.RankingLeague, pm.Page)
		if fetchErr != nil {
			if strings.Contains(fetchErr.Error(), "405") {
				log.Printf("[league] pg=%d worker=%d GOT 405 — pausando %s", pm.Page, workerID, retryPause)
				time.Sleep(retryPause)
				continue // NÃO commita
			}
			log.Printf("[league] pg=%d worker=%d erro: %v (skipping)", pm.Page, workerID, fetchErr)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		log.Printf("[league] pg=%d/%d worker=%d entries=%d took=%s",
			pm.Page, pm.TotalPages, workerID, len(pg.Entries), time.Since(reqStart).Round(time.Millisecond))

		s.persistPage(ctx, pg.Entries)

		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("[league] worker=%d commit erro: %v", workerID, err)
		}

		s.updateProgress(ctx, pm)
	}
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

var progressTicker atomic.Int64

func (s *Service) updateProgress(ctx context.Context, pm kafkainfra.LeaguePageMessage) {
	if progressTicker.Add(1)%50 != 0 {
		return
	}
	// synced_pages derivado: total de players / 10 (10 por página)
	// Aqui usamos CountPlayers como proxy. Não é exato (mesmo player em páginas
	// diferentes é único), mas dá uma referência de progresso.
	count, err := s.repo.CountPlayers(ctx)
	if err != nil {
		log.Printf("[league] count players erro: %v", err)
		return
	}
	synced := count / 10
	if synced > pm.TotalPages {
		synced = pm.TotalPages
	}

	status := "running"
	var finishedAt int64
	if synced >= pm.TotalPages {
		status = "done"
		finishedAt = time.Now().Unix()
	}

	_ = s.repo.SaveMeta(ctx, domain.SyncMeta{
		TotalCount:   pm.TotalCount,
		TotalPages:   pm.TotalPages,
		SyncedPages:  synced,
		UpdatedAt:    time.Now().Unix(),
		StartedAt:    pm.StartedAt,
		LastSyncedAt: finishedAt,
		Status:       status,
	})
	log.Printf("[league] progresso %d/%d páginas (%.1f%%) — %d players",
		synced, pm.TotalPages, float64(synced)/float64(pm.TotalPages)*100, count)

	if status == "done" {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		log.Printf("[league] sync COMPLETO")
	}
}

// ── Read methods (pra handlers) ──────────────────────────────────────────────

func (s *Service) GetMeta(ctx context.Context) (*domain.SyncMeta, error) {
	return s.repo.GetMeta(ctx)
}

func (s *Service) PlayersByCountry(ctx context.Context) ([]domain.CountryPlayerCount, error) {
	return s.repo.PlayersByCountry(ctx)
}
