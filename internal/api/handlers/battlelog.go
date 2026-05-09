package handlers

import (
	"encoding/json"
	"log"
	"net/http"

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

// Ensure domain.Battlelog is picked up by swag even though it's not in this package.
var _ = domain.Battlelog{}
