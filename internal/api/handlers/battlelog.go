package handlers

import (
	"context"
	"time"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	app "neo-shadaloo/internal/application/battlelog"
	domain "neo-shadaloo/internal/domain/battlelog"
	"neo-shadaloo/internal/infrastructure/realtime"
)

// GetBattlelog godoc
//
//	@Summary		Get battlelog for a user
//	@Description	Returns the cached battlelog from the database. If the cache is stale (>5 min), a background sync with the SF6 API is triggered without blocking the response. If no data exists yet, an empty replay list is returned and sync is triggered.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string				true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{object}	domain.Battlelog
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId} [get]
func GetBattlelog(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}

		bl, err := svc.GetBattlelog(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetBattlelog error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(bl)
	}
}

// PostSync godoc
//
//	@Summary		Force a battlelog sync
//	@Description	Triggers an immediate background sync with the SF6 API for the given user, regardless of cache freshness. Returns 202 immediately; the sync runs asynchronously and notifies connected WebSocket clients when complete.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string			true	"SF6 fighter ID"	example(3378249682)
//	@Success		202		{object}	map[string]string	"sync started"
//	@Failure		400		{string}	string	"userId required"
//	@Router			/v1/battlelog/{userId}/sync [post]
func PostSync(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}

		svc.ForceSync(userID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"sync started"}`))
	}
}

// GetWS godoc
//
//	@Summary		Subscribe to real-time battlelog updates
//	@Description	Upgrades the connection to WebSocket. The server sends a JSON message `{"type":"update","cachedAt":<ms>}` whenever a sync completes for this user. The client should re-fetch `/v1/battlelog/{userId}` on receipt. A ping is sent every 30 seconds to keep the connection alive.
//	@Tags			battlelog
//	@Param			userId	path	string	true	"SF6 fighter ID"	example(3378249682)
//	@Success		101
//	@Failure		400	{string}	string	"userId required"
//	@Router			/v1/battlelog/{userId}/ws [get]
func GetWS(hub *realtime.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		hub.ServeWS(userID, w, r)
	}
}

// GetProfile godoc
//
//	@Summary		Get profile/banner info for a user
//	@Description	Returns only the fighter banner info from the cached battlelog. Lighter alternative to the full battlelog endpoint.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string						true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{object}	domain.FighterBannerInfo
//	@Failure		400		{string}	string	"userId required"
//	@Failure		404		{string}	string	"no profile data yet"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/profile [get]
func GetProfile(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}

		bl, err := svc.GetBattlelog(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetProfile error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if bl.BannerInfo == nil {
			http.Error(w, "no profile data yet", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(bl.BannerInfo)
	}
}

// Health godoc
//
//	@Summary		Health check
//	@Description	Returns 200 if the server is running.
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/v1/health [get]
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}
}

// GetStats godoc
//
//	@Summary		Get win/loss stats for a user
//	@Description	Returns aggregated win/loss statistics computed from the cached battlelog.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string					true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{object}	domain.WinLossStat
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/stats [get]
func GetStats(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		stat, err := svc.ComputeStats(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetStats error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(stat)
	}
}

// GetOpponents godoc
//
//	@Summary		Get per-opponent-character stats for a user
//	@Description	Returns stats grouped by opponent character, sorted by total battles descending. Includes priority score for training recommendations.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string					true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{array}		domain.CharStat
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/opponents [get]
func GetOpponents(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		stats, err := svc.ComputeOpponents(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetOpponents error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(stats)
	}
}

// GetCalendar godoc
//
//	@Summary		Get calendar heatmap data for a user
//	@Description	Returns battles grouped by calendar day (YYYY-MM-DD) and by weekday (0=Sun..6=Sat).
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string					true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{object}	domain.CalendarStat
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/calendar [get]
func GetCalendar(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		cal, err := svc.ComputeCalendar(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetCalendar error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(cal)
	}
}

// GetReplaysPage godoc
//
//	@Summary		Get paginated replays for a user
//	@Description	Returns a paginated list of replays from the cached battlelog. Triggers a background sync if the cache is stale. Query params: page (default 1), limit (default 20, max 100).
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string				true	"SF6 fighter ID"	example(3378249682)
//	@Param			page	query		int					false	"Page number"		default(1)
//	@Param			limit	query		int					false	"Items per page"	default(20)
//	@Success		200		{object}	domain.ReplayPage
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/replays [get]
func GetReplaysPage(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}

		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			if v, err := strconv.Atoi(p); err == nil && v > 0 {
				page = v
			}
		}

		limit := 20
		if l := r.URL.Query().Get("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 {
				if v > 100 {
					v = 100
				}
				limit = v
			}
		}

		var f domain.ReplayFilter
		f.Character = r.URL.Query().Get("character")
		if v, err := strconv.ParseInt(r.URL.Query().Get("date_from"), 10, 64); err == nil {
			f.DateFrom = v
		}
		if v, err := strconv.ParseInt(r.URL.Query().Get("date_to"), 10, 64); err == nil {
			f.DateTo = v
		}
		if v, err := strconv.Atoi(r.URL.Query().Get("battle_type")); err == nil {
			f.BattleType = v
		}

		rp, err := svc.GetReplaysPage(r.Context(), userID, page, limit, f)
		if err != nil {
			log.Printf("[handler] GetReplaysPage error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(rp)
	}
}

// GetHourlyStats godoc
//
//	@Summary		Get hourly win-rate heatmap for a user
//	@Description	Returns win/loss counts grouped by hour of day (0–23) for a heatmap chart.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string				true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{object}	domain.HourlyStats
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/hourly [get]
func GetHourlyStats(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		stats, err := svc.ComputeHourlyStats(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetHourlyStats error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(stats)
	}
}

// GetLPHistory godoc
//
//	@Summary		Get LP evolution history for a user
//	@Description	Returns daily LP values (last match per day) sorted oldest-first, for plotting an LP progression chart.
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string				true	"SF6 fighter ID"	example(3378249682)
//	@Success		200		{object}	domain.LPHistory
//	@Failure		400		{string}	string	"userId required"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/battlelog/{userId}/lp-history [get]
func GetLPHistory(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		character := r.URL.Query().Get("character")
		hist, err := svc.ComputeLPHistory(r.Context(), userID, character)
		if err != nil {
			log.Printf("[handler] GetLPHistory error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(hist)
	}
}

// Ensure domain types are picked up by swag even though they're not in this package.
var _ = domain.Battlelog{}
var _ = domain.ReplayPage{}
var _ = domain.WinLossStat{}
var _ = domain.CharStat{}
var _ = domain.CalendarStat{}
var _ = domain.LPHistory{}
var _ = domain.HourlyStats{}

// GetCharacterRanks godoc
//
//	@Summary		Get most recent LP and rank per character for a user
//	@Tags			battlelog
//	@Produce		json
//	@Param			userId	path		string	true	"SF6 fighter ID"
//	@Success		200		{array}		domain.CharacterRankStat
//	@Router			/v1/battlelog/{userId}/character-ranks [get]
func GetCharacterRanks(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		ranks, err := svc.ComputeCharacterRanks(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetCharacterRanks error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(ranks)
	}
}

// GetPlayerSearch searches the player index by fighter_id.
func GetPlayerSearch(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]domain.PlayerEntry{})
			return
		}
		results, err := svc.SearchPlayers(r.Context(), q)
		if err != nil {
			log.Printf("[handler] SearchPlayers error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if results == nil {
			results = []domain.PlayerEntry{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(results)
	}
}

// PostPlayerReindex rebuilds the entire player search index from all saved battlelogs.
func PostPlayerReindex(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			n, err := svc.ReindexAll(ctx)
			if err != nil {
				log.Printf("[handler] ReindexAll error: %v", err)
			} else {
				log.Printf("[handler] ReindexAll complete: %d battlelogs indexed", n)
			}
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"reindex started"}`))
	}
}

// PostSyncAll triggers a background sync for all users in the player index.
func PostSyncAll(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		batchStr := r.URL.Query().Get("batch")
		batchSize := 5
		if b, err := strconv.Atoi(batchStr); err == nil && b > 0 {
			batchSize = b
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			synced, skipped, err := svc.SyncAll(ctx, batchSize)
			if err != nil {
				log.Printf("[sync-all] error: %v", err)
			} else {
				log.Printf("[sync-all] handler done — synced=%d skipped=%d", synced, skipped)
			}
		}()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"sync-all started"}`))
	}
}

// GetCharacters returns unique characters played by the user, sorted by play count.
func GetCharacters(svc *app.BattlelogService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userId")
		if userID == "" {
			http.Error(w, "userId required", http.StatusBadRequest)
			return
		}
		opts, err := svc.GetCharacters(r.Context(), userID)
		if err != nil {
			log.Printf("[handler] GetCharacters error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if opts == nil {
			opts = []domain.CharacterOption{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(opts)
	}
}
