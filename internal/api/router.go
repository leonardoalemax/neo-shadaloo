package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	app "neo-shadaloo/internal/application/battlelog"
	appfighting "neo-shadaloo/internal/application/fighting"
	appleague "neo-shadaloo/internal/application/league"
	appranking "neo-shadaloo/internal/application/ranking"
	appusage "neo-shadaloo/internal/application/usage"
	"neo-shadaloo/internal/api/handlers"
	"neo-shadaloo/internal/infrastructure/realtime"
)

func NewRouter(svc *app.BattlelogService, usageSvc *appusage.UsageService, fightingSvc *appfighting.FightingService, rankingSvc *appranking.Service, leagueSvc *appleague.Service, hub *realtime.Hub) http.Handler {
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

	// Ranking global (snapshot atual). 4 tipos: league_point, arcade_score, kudos, master_rating
	r.Post("/v1/ranking/sync", handlers.PostRankingSyncAll(rankingSvc))
	r.Post("/v1/ranking/{type}/sync", handlers.PostRankingSync(rankingSvc))
	r.Get("/v1/ranking/{type}/status", handlers.GetRankingStatus(rankingSvc))
	// Visualização
	r.Get("/v1/ranking/{type}", handlers.GetRanking(rankingSvc))
	r.Get("/v1/ranking/{type}/facets", handlers.GetRankingFacets(rankingSvc))
	r.Get("/v1/ranking/{type}/player/{short_id}", handlers.GetRankingByPlayer(rankingSvc))
	r.Get("/v1/ranking/{type}/around/{order}", handlers.GetRankingAround(rankingSvc))
	r.Get("/v1/ranking/{type}/players-by-country", handlers.GetRankingPlayersByCountry(rankingSvc))

	// League (ranking dedicado, tabela league_player com upsert por short_id)
	r.Post("/v1/league/sync", handlers.PostLeagueSync(leagueSvc))
	r.Get("/v1/league/status", handlers.GetLeagueStatus(leagueSvc))
	r.Get("/v1/league/players-by-country", handlers.GetLeaguePlayersByCountry(leagueSvc))
	r.Get("/v1/league/characters", handlers.GetLeagueCharacters(leagueSvc))
	r.Get("/v1/league/ranks", handlers.GetLeagueRanks(leagueSvc))

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
