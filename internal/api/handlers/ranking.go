package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "neo-shadaloo/internal/application/ranking"
	domain "neo-shadaloo/internal/domain/ranking"
)

// PostRankingSyncAll dispara o sync de TODOS os 4 rankings em background.
//
//	@Summary		Dispara sync de todos os rankings globais
//	@Description	Faz o crawl de todas as páginas dos 4 rankings (league, arcade, kudos, master) em background. Snapshot atual — substitui dados anteriores.
//	@Tags			ranking
//	@Produce		json
//	@Success		202	{object}	map[string]string
//	@Router			/v1/ranking/sync [post]
func PostRankingSyncAll(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc.TriggerSyncAll()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "sync iniciado em background para todos os rankings",
		})
	}
}

// PostRankingSync dispara o sync de UM ranking específico.
//
//	@Summary		Dispara sync de um ranking específico
//	@Param			type	path	string	true	"Tipo de ranking"	Enums(league_point, arcade_score, kudos, master_rating)
//	@Tags			ranking
//	@Produce		json
//	@Success		202	{object}	map[string]string
//	@Failure		400	{object}	map[string]string
//	@Router			/v1/ranking/{type}/sync [post]
func PostRankingSync(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := domain.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		svc.TriggerSync(rt)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":       "sync iniciado em background",
			"ranking_type": string(rt),
		})
	}
}

// GetRankingStatus retorna o estado atual do sync de um ranking.
//
//	@Summary		Estado de sync de um ranking
//	@Param			type	path	string	true	"Tipo de ranking"
//	@Tags			ranking
//	@Produce		json
//	@Success		200	{object}	domain.SnapshotMeta
//	@Router			/v1/ranking/{type}/status [get]
func GetRankingStatus(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := domain.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		meta, err := svc.GetMeta(r.Context(), rt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if meta == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ranking_type": string(rt),
				"status":       "never_synced",
				"running":      svc.IsRunning(rt),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ranking_type":    string(meta.RankingType),
			"total_count":     meta.TotalCount,
			"total_pages":     meta.TotalPages,
			"synced_pages":    meta.SyncedPages,
			"updated_at":      meta.UpdatedAt,
			"started_at":      meta.StartedAt,
			"last_synced_at":  meta.LastSyncedAt,
			"status":          meta.Status,
			"running":         svc.IsRunning(rt),
		})
	}
}

func isValidRankingType(rt domain.RankingType) bool {
	for _, valid := range domain.AllRankingTypes() {
		if rt == valid {
			return true
		}
	}
	return false
}
