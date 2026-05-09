package sf6

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sync"

	domainconfig "neo-shadaloo/internal/domain/config"
)

const (
	buildIDKey     = "sf6_build_id"
	buildIDProfile = "0"
)

var buildIDRe = regexp.MustCompile(`"buildId"\s*:\s*"([^"]+)"`)

type buildIDManager struct {
	mu        sync.Mutex
	cfgRepo   domainconfig.ConfigRepository
	userAgent string
}

func newBuildIDManager(cfgRepo domainconfig.ConfigRepository, userAgent string) *buildIDManager {
	return &buildIDManager{cfgRepo: cfgRepo, userAgent: userAgent}
}

// Get returns the cached build ID from the DB, fetching from the SF6 site if missing.
func (m *buildIDManager) Get(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, err := m.cfgRepo.Get(ctx, buildIDKey)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	return m.refreshLocked(ctx)
}

// Refresh forces a re-fetch from the SF6 site and persists the new ID.
func (m *buildIDManager) Refresh(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshLocked(ctx)
}

func (m *buildIDManager) refreshLocked(ctx context.Context) (string, error) {
	log.Println("[sf6] Fetching fresh buildId from SF6 site...")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		sf6Base+"/en/profile/"+buildIDProfile+"/battlelog", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept", "text/html")
	req.Header.Set("user-agent", m.userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("buildId fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	match := buildIDRe.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("buildId not found in SF6 HTML")
	}

	id := string(match[1])
	log.Printf("[sf6] Got buildId: %s", id)

	if err := m.cfgRepo.Set(ctx, buildIDKey, id); err != nil {
		return "", err
	}
	return id, nil
}
