package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

const (
	TopicRankingSync = "ranking-sync-pages"
	TopicLeagueSync  = "league-sync-pages"
)

// EnsureTopicNamed cria um topic arbitrário com N partições.
func EnsureTopicNamed(ctx context.Context, topic string, partitions int) error {
	conn, err := kafka.DialLeader(ctx, "tcp", BrokerAddr(), topic, 0)
	if err != nil {
		ctrlConn, cErr := kafka.Dial("tcp", BrokerAddr())
		if cErr != nil {
			return fmt.Errorf("kafka dial: %w", cErr)
		}
		defer ctrlConn.Close()
		return ctrlConn.CreateTopics(kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: 1,
		})
	}
	conn.Close()
	return nil
}

// NewWriterFor cria um writer pra um topic específico.
func NewWriterFor(topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(BrokerAddr()),
		Topic:        topic,
		Balancer:     &kafka.RoundRobin{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
	}
}

// NewReaderFor cria um reader pra um topic específico.
func NewReaderFor(topic, groupID string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{BrokerAddr()},
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafka.FirstOffset,
	})
}

// LeaguePageMessage é o payload da fila de league.
type LeaguePageMessage struct {
	Page       int   `json:"page"`
	TotalPages int   `json:"total_pages"`
	TotalCount int   `json:"total_count"`
	StartedAt  int64 `json:"started_at"`
}

// ProduceLeaguePages envia mensagens pra fila de league.
func ProduceLeaguePages(ctx context.Context, w *kafka.Writer, startPage, totalPages, totalCount int, startedAt int64) error {
	if totalPages < startPage {
		return nil
	}
	msgs := make([]kafka.Message, 0, totalPages-startPage+1)
	for page := startPage; page <= totalPages; page++ {
		payload, _ := json.Marshal(LeaguePageMessage{
			Page: page, TotalPages: totalPages, TotalCount: totalCount, StartedAt: startedAt,
		})
		msgs = append(msgs, kafka.Message{
			Key:   []byte(fmt.Sprintf("league-%d", page)),
			Value: payload,
		})
	}
	batchSize := 500
	for i := 0; i < len(msgs); i += batchSize {
		end := i + batchSize
		if end > len(msgs) {
			end = len(msgs)
		}
		if err := w.WriteMessages(ctx, msgs[i:end]...); err != nil {
			return fmt.Errorf("kafka produce league pages %d-%d: %w", i+startPage, end+startPage-1, err)
		}
		log.Printf("[kafka] produced league pages %d-%d/%d", i+startPage, end+startPage-1, totalPages)
	}
	return nil
}

// PageMessage representa uma página de ranking a ser buscada.
type PageMessage struct {
	RankingType string `json:"ranking_type"`
	Page        int    `json:"page"`
	TotalPages  int    `json:"total_pages"`
	TotalCount  int    `json:"total_count"`
	StartedAt   int64  `json:"started_at"`
}

// BrokerAddr retorna o endereço do broker Kafka via ENV.
func BrokerAddr() string {
	addr := os.Getenv("KAFKA_BROKER")
	if addr == "" {
		addr = "localhost:9092"
	}
	log.Printf("[kafka] broker addr: %s", addr)
	return addr
}

// EnsureTopic cria o topic se não existir.
func EnsureTopic(ctx context.Context) error {
	conn, err := kafka.DialLeader(ctx, "tcp", BrokerAddr(), TopicRankingSync, 0)
	if err != nil {
		// Se não conseguir conectar, tenta criar via controlador
		ctrlConn, cErr := kafka.Dial("tcp", BrokerAddr())
		if cErr != nil {
			return fmt.Errorf("kafka dial: %w", cErr)
		}
		defer ctrlConn.Close()

		return ctrlConn.CreateTopics(kafka.TopicConfig{
			Topic:             TopicRankingSync,
			NumPartitions:     5, // 5 workers = 5 partitions
			ReplicationFactor: 1,
		})
	}
	conn.Close()
	return nil
}

// NewWriter cria um writer pro topic de ranking sync.
func NewWriter() *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(BrokerAddr()),
		Topic:        TopicRankingSync,
		Balancer:     &kafka.RoundRobin{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
	}
}

// NewReader cria um reader (consumer group) pro topic de ranking sync.
func NewReader(groupID string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{BrokerAddr()},
		Topic:          TopicRankingSync,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafka.FirstOffset,
	})
}

// ProducePages envia N mensagens (uma por página) pro Kafka.
func ProducePages(ctx context.Context, w *kafka.Writer, rankingType string, startPage, totalPages, totalCount int, startedAt int64) error {
	msgs := make([]kafka.Message, 0, totalPages-startPage+1)
	for page := startPage; page <= totalPages; page++ {
		payload, _ := json.Marshal(PageMessage{
			RankingType: rankingType,
			Page:        page,
			TotalPages:  totalPages,
			TotalCount:  totalCount,
			StartedAt:   startedAt,
		})
		msgs = append(msgs, kafka.Message{
			Key:   []byte(fmt.Sprintf("%s-%d", rankingType, page)),
			Value: payload,
		})
	}

	// Envia em batches de 500 pra não estourar memória
	batchSize := 500
	for i := 0; i < len(msgs); i += batchSize {
		end := i + batchSize
		if end > len(msgs) {
			end = len(msgs)
		}
		if err := w.WriteMessages(ctx, msgs[i:end]...); err != nil {
			return fmt.Errorf("kafka produce pages %d-%d: %w", i+startPage, end+startPage-1, err)
		}
		log.Printf("[kafka] produced pages %d-%d/%d for %s", i+startPage, end+startPage-1, totalPages, rankingType)
	}
	return nil
}
