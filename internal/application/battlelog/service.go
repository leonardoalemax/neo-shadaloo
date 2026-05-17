package battlelog

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	domain "neo-shadaloo/internal/domain/battlelog"
)

// BattlelogService orchestrates battlelog use cases.
type BattlelogService struct {
	repo    domain.BattlelogRepository
	players domain.PlayerRepository
	sf6     domain.SF6Client
	publisher domain.EventPublisher

	mu      sync.Mutex
	syncing map[string]bool
}

func NewBattlelogService(
	repo domain.BattlelogRepository,
	players domain.PlayerRepository,
	sf6 domain.SF6Client,
	publisher domain.EventPublisher,
) *BattlelogService {
	return &BattlelogService{
		repo:    repo,
		players: players,
		sf6:     sf6,
		publisher: publisher,
		syncing: make(map[string]bool),
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
		go s.players.MarkSyncable(context.Background(), userID) //nolint
		return &domain.Battlelog{UserID: userID, Replays: []domain.Replay{}}, nil
	}

	if bl.IsStale() {
		s.TriggerSync(userID)
	}

	go s.players.MarkSyncable(context.Background(), userID) //nolint
	return bl, nil
}

// GetReplaysPage returns a paginated slice of replays from the cached battlelog.
// It reuses GetBattlelog, so stale-cache detection and background sync apply as usual.
func (s *BattlelogService) GetReplaysPage(ctx context.Context, userID string, page, limit int, f domain.ReplayFilter) (*domain.ReplayPage, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	replays := bl.Replays
	if f.Character != "" || f.DateFrom != 0 || f.DateTo != 0 || f.BattleType != 0 {
		filtered := replays[:0:0]
		for _, r := range replays {
			if f.Character != "" {
				side := domain.FindUserSide(r, userID)
				var charTool string
				if side == 1 {
					charTool = r.Player1Info.PlayingCharacterToolName
				} else if side == 2 {
					charTool = r.Player2Info.PlayingCharacterToolName
				}
				if charTool != f.Character {
					continue
				}
			}
			if f.DateFrom != 0 && r.UploadedAt < f.DateFrom {
				continue
			}
			if f.DateTo != 0 && r.UploadedAt > f.DateTo {
				continue
			}
			if f.BattleType != 0 && r.ReplayBattleType != f.BattleType {
				continue
			}
			filtered = append(filtered, r)
		}
		replays = filtered
	}

	total := len(replays)
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	return &domain.ReplayPage{
		Replays:    replays[start:end],
		Total:      total,
		Page:       page,
		TotalPages: totalPages,
	}, nil
}

// GetCharacters returns the unique characters played by userID, sorted by play count desc.
func (s *BattlelogService) GetCharacters(ctx context.Context, userID string) ([]domain.CharacterOption, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	names := make(map[string]string)
	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		var info domain.PlayerInfo
		if side == 1 {
			info = r.Player1Info
		} else if side == 2 {
			info = r.Player2Info
		} else {
			continue
		}
		key := info.PlayingCharacterToolName
		counts[key]++
		names[key] = info.PlayingCharacterName
	}

	opts := make([]domain.CharacterOption, 0, len(counts))
	for tool, name := range names {
		opts = append(opts, domain.CharacterOption{ToolName: tool, Name: name})
	}
	sort.Slice(opts, func(i, j int) bool {
		return counts[opts[i].ToolName] > counts[opts[j].ToolName]
	})
	return opts, nil
}

// ComputeStats returns aggregated win/loss stats for userID from the cached battlelog.
func (s *BattlelogService) ComputeStats(ctx context.Context, userID string) (*domain.WinLossStat, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	var wins, losses int
	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}
		if domain.GetWinner(r) == side {
			wins++
		} else {
			losses++
		}
	}

	total := wins + losses
	winPct := 0
	if total > 0 {
		winPct = int(float64(wins) / float64(total) * 100)
	}

	return &domain.WinLossStat{Wins: wins, Losses: losses, Total: total, WinPct: winPct}, nil
}

// ComputeOpponents returns per-opponent-character stats sorted by total battles desc.
// If character is non-empty, only replays where the user played that character are included.
func (s *BattlelogService) ComputeOpponents(ctx context.Context, userID string, character string) ([]domain.CharStat, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	statsMap := make(map[string]*domain.CharStat)

	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}

		var opponent domain.PlayerInfo
		var userInfo domain.PlayerInfo
		if side == 1 {
			opponent = r.Player2Info
			userInfo = r.Player1Info
		} else {
			opponent = r.Player1Info
			userInfo = r.Player2Info
		}

		// Filter by user's character if requested
		if character != "" && userInfo.PlayingCharacterToolName != character {
			continue
		}

		key := opponent.PlayingCharacterToolName
		if _, ok := statsMap[key]; !ok {
			statsMap[key] = &domain.CharStat{
				Name:     opponent.PlayingCharacterName,
				ToolName: key,
			}
		}

		stat := statsMap[key]
		stat.Total++

		won := domain.GetWinner(r) == side
		if won {
			stat.Wins++
		} else {
			stat.Losses++
			roundsWon := 0
			for _, v := range userInfo.RoundResults {
				if v > 0 {
					roundsWon++
				}
			}
			if roundsWon == 0 {
				stat.CleanLosses++
			} else {
				stat.CloseLosses++
			}
		}

		stat.WinRate = int(float64(stat.Wins) / float64(stat.Total) * 100)

		weight := 1.0
		if stat.WinRate >= 50 {
			weight = 0.5
		}
		stat.PriorityScore = (float64(stat.CleanLosses)*3.0 + float64(stat.CloseLosses)*1.5) * weight
	}

	result := make([]domain.CharStat, 0, len(statsMap))
	for _, s := range statsMap {
		result = append(result, *s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Total > result[j].Total
	})

	return result, nil
}

// ComputeCalendar returns battles grouped by calendar day and weekday.
func (s *BattlelogService) ComputeCalendar(ctx context.Context, userID string) (*domain.CalendarStat, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	byDay := make(map[string]domain.DayStat)
	var byWeekday [7]domain.DayStat

	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}

		t := time.Unix(r.UploadedAt, 0)
		key := t.Format("2006-01-02")
		won := domain.GetWinner(r) == side

		day := byDay[key]
		day.Total++
		if won {
			day.Wins++
		}
		byDay[key] = day

		wd := int(t.Weekday())
		wds := byWeekday[wd]
		wds.Total++
		if won {
			wds.Wins++
		}
		byWeekday[wd] = wds
	}

	return &domain.CalendarStat{ByDay: byDay, ByWeekday: byWeekday}, nil
}

// ComputeHourlyStats returns win/loss counts grouped by hour of day (0–23).
func (s *BattlelogService) ComputeHourlyStats(ctx context.Context, userID string) (*domain.HourlyStats, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	var result domain.HourlyStats
	for h := range result.Hours {
		result.Hours[h].Hour = h
	}

	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}
		h := time.Unix(r.UploadedAt, 0).Hour()
		result.Hours[h].Total++
		if domain.GetWinner(r) == side {
			result.Hours[h].Wins++
		}
	}

	return &result, nil
}

// ComputeWeeklyHeatmap returns win/loss grouped by (weekday, hour).
func (s *BattlelogService) ComputeWeeklyHeatmap(ctx context.Context, userID string) (*domain.WeeklyHeatmap, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	var result domain.WeeklyHeatmap
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			result.Days[d][h].Hour = h
		}
	}

	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}
		t := time.Unix(r.UploadedAt, 0)
		dow := int(t.Weekday()) // 0=Sun
		h := t.Hour()
		result.Days[dow][h].Total++
		if domain.GetWinner(r) == side {
			result.Days[dow][h].Wins++
		}
	}

	return &result, nil
}

// ComputeLPHistory returns LP per day for userID filtered by character (playing_character_tool_name).
// Replays are sorted newest-first, so the first occurrence of each date is the last match of that day.
func (s *BattlelogService) ComputeLPHistory(ctx context.Context, userID, character string) (*domain.LPHistory, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	// replays are newest-first; first hit per date = last match of that day
	seenDate := make(map[string]bool)
	type entry struct {
		date string
		lp   int
		ts   int64
	}
	var entries []entry

	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}
		var info domain.PlayerInfo
		if side == 1 {
			info = r.Player1Info
		} else {
			info = r.Player2Info
		}
		if character != "" && info.PlayingCharacterToolName != character {
			continue
		}
		date := time.Unix(r.UploadedAt, 0).Format("2006-01-02")
		if seenDate[date] {
			continue
		}
		seenDate[date] = true
		entries = append(entries, entry{date: date, lp: info.LeaguePoint, ts: r.UploadedAt})
	}

	// sort oldest-first for the chart
	sort.Slice(entries, func(i, j int) bool { return entries[i].ts < entries[j].ts })

	result := make([]domain.LPEntry, len(entries))
	for i, e := range entries {
		result[i] = domain.LPEntry{Date: e.date, LP: e.lp}
	}
	return &domain.LPHistory{Entries: result}, nil
}

// ComputeCharacterRanks returns the most recent LP and rank for each character played by userID.
// Results are sorted by LP descending.
func (s *BattlelogService) ComputeCharacterRanks(ctx context.Context, userID string) ([]domain.CharacterRankStat, error) {
	bl, err := s.GetBattlelog(ctx, userID)
	if err != nil {
		return nil, err
	}

	type snapshot struct {
		name   string
		lp     int
		rank   int
		ts     int64
	}
	best := make(map[string]snapshot)

	// replays are newest-first; first hit per character = most recent match
	for _, r := range bl.Replays {
		side := domain.FindUserSide(r, userID)
		if side == 0 {
			continue
		}
		var info domain.PlayerInfo
		if side == 1 {
			info = r.Player1Info
		} else {
			info = r.Player2Info
		}
		tool := info.PlayingCharacterToolName
		if tool == "" {
			continue
		}
		if _, seen := best[tool]; !seen {
			best[tool] = snapshot{
				name: info.PlayingCharacterName,
				lp:   info.LeaguePoint,
				rank: info.LeagueRank,
				ts:   r.UploadedAt,
			}
		}
	}

	result := make([]domain.CharacterRankStat, 0, len(best))
	for tool, s := range best {
		result = append(result, domain.CharacterRankStat{
			Name:       s.name,
			ToolName:   tool,
			LP:         s.lp,
			LeagueRank: s.rank,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LP > result[j].LP })
	return result, nil
}

// indexPlayers upserts players + characters vistos no battlelog.
func (s *BattlelogService) indexPlayers(ctx context.Context, bl *domain.Battlelog) {
	now := time.Now().UnixMilli()

	// 1. Upsert do dono via BannerInfo (dados ricos)
	if bl.BannerInfo != nil && bl.BannerInfo.PersonalInfo.ShortID > 0 {
		b := bl.BannerInfo
		if err := s.players.UpsertFromBanner(ctx, domain.Player{
			ShortID:                   b.PersonalInfo.ShortID,
			FighterID:                 b.PersonalInfo.FighterID,
			PlatformName:              b.PersonalInfo.PlatformName,
			PlatformToolName:          b.PersonalInfo.PlatformToolName,
			HomeID:                    b.HomeID,
			FavoriteCharacterToolName: b.FavoriteCharacterToolName,
			FavoriteCharacterName:     b.FavoriteCharacterName,
			LeaguePoint:              b.FavoriteCharacterLeagueInfo.LeaguePoint,
			LeagueRank:               b.FavoriteCharacterLeagueInfo.LeagueRank,
			TitlePlateName:           b.TitleData.TitleDataPlateName,
			TitleVal:                 b.TitleData.TitleDataVal,
			PPFightingGround:         b.FavoriteCharacterPlayPoint.FightingGround,
			PPWorldTour:              b.FavoriteCharacterPlayPoint.WorldTour,
			PPBattleHub:              b.FavoriteCharacterPlayPoint.BattleHub,
			UpdatedAt:                now,
		}); err != nil {
			log.Printf("[player] banner upsert error for %s: %v", bl.UserID, err)
		}
	}

	// 2. Coletar players e characters dos replays
	seenPlayers := make(map[int64]domain.Player)
	// charKey = "shortID|charToolName"
	seenChars := make(map[string]domain.PlayerCharacter)

	for _, r := range bl.Replays {
		for _, pi := range []domain.PlayerInfo{r.Player1Info, r.Player2Info} {
			sid := pi.Player.ShortID
			if sid == 0 || pi.Player.FighterID == "" {
				continue
			}

			// Player (dados básicos do replay)
			if _, ok := seenPlayers[sid]; !ok {
				seenPlayers[sid] = domain.Player{
					ShortID:          sid,
					FighterID:        pi.Player.FighterID,
					PlatformID:       pi.Player.PlatformID,
					PlatformName:     pi.Player.PlatformName,
					PlatformToolName: pi.Player.PlatformToolName,
					LeaguePoint:      pi.LeaguePoint,
					LeagueRank:       pi.LeagueRank,
					UpdatedAt:        r.UploadedAt,
				}
			}

			// Character (personagem usado nesse replay)
			charKey := fmt.Sprintf("%d|%s", sid, pi.PlayingCharacterToolName)
			if pi.PlayingCharacterToolName != "" {
				if existing, ok := seenChars[charKey]; !ok || r.UploadedAt > existing.LastSeenAt {
					seenChars[charKey] = domain.PlayerCharacter{
						ShortID:           sid,
						CharacterToolName: pi.PlayingCharacterToolName,
						CharacterName:     pi.PlayingCharacterName,
						LeaguePoint:       pi.LeaguePoint,
						LeagueRank:        pi.LeagueRank,
						LastSeenAt:        r.UploadedAt,
					}
				}
			}
		}
	}

	// 3. Upsert players
	players := make([]domain.Player, 0, len(seenPlayers))
	for _, p := range seenPlayers {
		players = append(players, p)
	}
	if err := s.players.UpsertFromReplay(ctx, players); err != nil {
		log.Printf("[player] replay upsert error for %s: %v", bl.UserID, err)
	}

	// 4. Upsert characters
	chars := make([]domain.PlayerCharacter, 0, len(seenChars))
	for _, c := range seenChars {
		chars = append(chars, c)
	}
	if err := s.players.UpsertCharacters(ctx, chars); err != nil {
		log.Printf("[player] characters upsert error for %s: %v", bl.UserID, err)
	}

	log.Printf("[player] indexed %d players + %d characters from %s",
		len(players), len(chars), bl.UserID)
}

// SearchPlayers returns players matching the query string.
func (s *BattlelogService) SearchPlayers(ctx context.Context, query string) ([]domain.Player, error) {
	return s.players.Search(ctx, query)
}

// GetPlayer returns a player by fighter_id.
func (s *BattlelogService) GetPlayer(ctx context.Context, fighterID string) (*domain.Player, error) {
	return s.players.GetByFighterID(ctx, fighterID)
}

// GetPlayerCharacters returns characters used by a player.
func (s *BattlelogService) GetPlayerCharacters(ctx context.Context, shortID int64) ([]domain.PlayerCharacter, error) {
	return s.players.GetCharacters(ctx, shortID)
}

// ReindexAll scans all saved battlelogs and rebuilds the player index.
func (s *BattlelogService) ReindexAll(ctx context.Context) (int, error) {
	userIDs, err := s.repo.ListAllUserIDs(ctx)
	if err != nil {
		return 0, err
	}

	total := 0
	for _, uid := range userIDs {
		bl, err := s.repo.GetByUserID(ctx, uid)
		if err != nil || bl == nil {
			continue
		}
		s.indexPlayers(ctx, bl)
		total++
	}
	return total, nil
}

// SyncAll syncs battlelogs for every user in the player index.
// It processes users in batches of batchSize, skipping any battlelog synced within skipIfNewerThan.
// Returns the number of users synced and skipped.
func (s *BattlelogService) SyncAll(ctx context.Context, batchSize int) (synced, skipped int, err error) {
	const skipIfNewerThan = 4 * time.Hour
	start := time.Now()

	userIDs, err := s.players.ListSyncableUserIDs(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("SyncAll: list syncable user IDs: %w", err)
	}

	log.Printf("[sync-all] starting — %d users, batch=%d", len(userIDs), batchSize)

	for i := 0; i < len(userIDs); i += batchSize {
		end := i + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		batch := userIDs[i:end]

		type result struct {
			userID  string
			skipped bool
			err     error
		}
		ch := make(chan result, len(batch))

		for _, uid := range batch {
			uid := uid
			go func() {
				// Check cached_at before syncing
				bl, err := s.repo.GetByUserID(ctx, uid)
				if err != nil {
					ch <- result{userID: uid, err: err}
					return
				}
				if bl != nil && time.Since(time.UnixMilli(bl.CachedAt)) < skipIfNewerThan {
					ch <- result{userID: uid, skipped: true}
					return
				}
				s.runSync(uid)
				ch <- result{userID: uid}
			}()
		}

		for range batch {
			r := <-ch
			if r.err != nil {
				log.Printf("[sync-all] error syncing %s: %v", r.userID, r.err)
			} else if r.skipped {
				skipped++
			} else {
				synced++
			}
		}

		log.Printf("[sync-all] batch %d–%d done (synced=%d skipped=%d elapsed=%s)",
			i+1, end, synced, skipped, time.Since(start).Round(time.Millisecond))
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	log.Printf("[sync-all] complete — synced=%d skipped=%d total=%d elapsed=%s",
		synced, skipped, len(userIDs), elapsed)

	return synced, skipped, nil
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

	s.indexPlayers(ctx, existing)

	s.publisher.Publish(domain.BattlelogSyncedEvent{
		UserID:   userID,
		CachedAt: existing.CachedAt,
	})
}
