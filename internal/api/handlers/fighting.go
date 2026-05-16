package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	appfighting "neo-shadaloo/internal/application/fighting"
)

func GetFightingMonths(svc *appfighting.FightingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		months, err := svc.GetAvailableMonths(r.Context())
		if err != nil {
			log.Printf("[handler] GetFightingMonths error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if months == nil {
			months = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(months)
	}
}

func GetFighting(svc *appfighting.FightingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		yyyymm := chi.URLParam(r, "yyyymm")
		if !yyyymmRe.MatchString(yyyymm) {
			http.Error(w, "invalid yyyymm", http.StatusBadRequest)
			return
		}
		snap, err := svc.GetFighting(r.Context(), yyyymm)
		if err != nil {
			log.Printf("[handler] GetFighting error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(snap)
	}
}

func PostFightingSync(svc *appfighting.FightingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		yyyymm := chi.URLParam(r, "yyyymm")
		if !yyyymmRe.MatchString(yyyymm) {
			http.Error(w, "invalid yyyymm", http.StatusBadRequest)
			return
		}
		svc.ForceSync(yyyymm)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"sync started"}`))
	}
}
