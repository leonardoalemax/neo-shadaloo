package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	app "neo-shadaloo/internal/application/battlelog"
	"neo-shadaloo/internal/api/handlers"
	"neo-shadaloo/internal/infrastructure/realtime"
)

func NewRouter(svc *app.BattlelogService, hub *realtime.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/v1/health", handlers.Health())
	r.Get("/v1/battlelog/{userId}", handlers.GetBattlelog(svc))
	r.Get("/v1/battlelog/{userId}/replays", handlers.GetReplaysPage(svc))
	r.Post("/v1/battlelog/{userId}/sync", handlers.PostSync(svc))
	r.Get("/v1/battlelog/{userId}/ws", handlers.GetWS(hub))

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
