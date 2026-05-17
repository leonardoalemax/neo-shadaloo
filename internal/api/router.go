package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	app "neo-shadaloo/internal/application/battlelog"
	appfighting "neo-shadaloo/internal/application/fighting"
	appusage "neo-shadaloo/internal/application/usage"
	"neo-shadaloo/internal/api/handlers"
	"neo-shadaloo/internal/infrastructure/realtime"
)

func NewRouter(svc *app.BattlelogService, usageSvc *appusage.UsageService, fightingSvc *appfighting.FightingService, hub *realtime.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/v1/health", handlers.Health())
	r.Get("/v1/battlelog/{userId}", handlers.GetBattlelog(svc))
	r.Get("/v1/battlelog/{userId}/profile", handlers.GetProfile(svc))
	r.Get("/v1/battlelog/{userId}/stats", handlers.GetStats(svc))
	r.Get("/v1/battlelog/{userId}/opponents", handlers.GetOpponents(svc))
	r.Get("/v1/battlelog/{userId}/calendar", handlers.GetCalendar(svc))
	r.Get("/v1/battlelog/{userId}/replays", handlers.GetReplaysPage(svc))
	r.Get("/v1/battlelog/{userId}/characters", handlers.GetCharacters(svc))
	r.Get("/v1/battlelog/{userId}/character-ranks", handlers.GetCharacterRanks(svc))
	r.Get("/v1/battlelog/{userId}/lp-history", handlers.GetLPHistory(svc))
	r.Get("/v1/battlelog/{userId}/hourly", handlers.GetHourlyStats(svc))
	r.Post("/v1/battlelog/{userId}/sync", handlers.PostSync(svc))
	r.Get("/v1/battlelog/{userId}/ws", handlers.GetWS(hub))

	r.Get("/v1/players/search", handlers.GetPlayerSearch(svc))
	r.Post("/v1/players/reindex", handlers.PostPlayerReindex(svc))
	r.Post("/v1/players/sync-all", handlers.PostSyncAll(svc))

	r.Get("/v1/usage/months", handlers.GetUsageMonths(usageSvc))
	r.Get("/v1/usage/{yyyymm}", handlers.GetUsage(usageSvc))
	r.Post("/v1/usage/{yyyymm}/sync", handlers.PostUsageSync(usageSvc))

	r.Get("/v1/fighting/months", handlers.GetFightingMonths(fightingSvc))
	r.Get("/v1/fighting/{yyyymm}", handlers.GetFighting(fightingSvc))
	r.Post("/v1/fighting/{yyyymm}/sync", handlers.PostFightingSync(fightingSvc))

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
