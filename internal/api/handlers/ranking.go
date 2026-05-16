package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	app "neo-shadaloo/internal/application/ranking"
	dranking "neo-shadaloo/internal/domain/ranking"
)

// referência usada por swag pra gerar o schema
var _ = dranking.ListPage{}
var _ = dranking.Facets{}

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
		rt := dranking.RankingType(chi.URLParam(r, "type"))
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
//	@Success		200	{object}	dranking.SnapshotMeta
//	@Router			/v1/ranking/{type}/status [get]
func GetRankingStatus(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := dranking.RankingType(chi.URLParam(r, "type"))
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

// GetRanking lista uma página do ranking com filtros opcionais.
//
//	@Summary		Lista paginada do ranking
//	@Param			type		path	string	true	"Tipo de ranking"	Enums(league_point, arcade_score, kudos, master_rating)
//	@Param			page		query	int		false	"Página (1-based)"
//	@Param			limit		query	int		false	"Items por página (max 200, default 50)"
//	@Param			character	query	string	false	"Filtrar por character_tool_name (ex: zangief)"
//	@Param			home_id		query	int		false	"Filtrar por região (home_id)"
//	@Tags			ranking
//	@Produce		json
//	@Success		200	{object}	dranking.ListPage
//	@Failure		400	{object}	map[string]string
//	@Router			/v1/ranking/{type} [get]
func GetRanking(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := dranking.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		homeID, _ := strconv.Atoi(q.Get("home_id"))

		out, err := svc.List(r.Context(), rt, dranking.ListFilter{
			Page:              page,
			Limit:             limit,
			CharacterToolName: q.Get("character"),
			HomeID:            homeID,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, out)
	}
}

// GetRankingByPlayer busca todas as entries de um jogador no ranking.
//
//	@Summary		Posição de um jogador no ranking
//	@Param			type		path	string	true	"Tipo de ranking"
//	@Param			short_id	path	int		true	"Short ID do jogador"
//	@Tags			ranking
//	@Produce		json
//	@Success		200	{array}	dranking.Entry
//	@Router			/v1/ranking/{type}/player/{short_id} [get]
func GetRankingByPlayer(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := dranking.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		shortID, err := strconv.ParseInt(chi.URLParam(r, "short_id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"short_id inválido"}`, http.StatusBadRequest)
			return
		}
		entries, err := svc.ByPlayer(r.Context(), rt, shortID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []dranking.Entry{}
		}
		writeJSON(w, entries)
	}
}

// GetRankingAround devolve entries em volta de uma posição (±radius).
//
//	@Summary		Vizinhança de uma posição no ranking
//	@Param			type	path	string	true	"Tipo de ranking"
//	@Param			order	path	int		true	"Posição central (order_no)"
//	@Param			radius	query	int		false	"Raio (default 5, max 100)"
//	@Tags			ranking
//	@Produce		json
//	@Success		200	{array}	dranking.Entry
//	@Router			/v1/ranking/{type}/around/{order} [get]
func GetRankingAround(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := dranking.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		order, err := strconv.Atoi(chi.URLParam(r, "order"))
		if err != nil || order < 1 {
			http.Error(w, `{"error":"order inválido"}`, http.StatusBadRequest)
			return
		}
		radius, _ := strconv.Atoi(r.URL.Query().Get("radius"))
		entries, err := svc.Around(r.Context(), rt, order, radius)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []dranking.Entry{}
		}
		writeJSON(w, entries)
	}
}

// GetRankingFacets devolve contadores por personagem e região (pra filtros do front).
//
//	@Summary		Facets do ranking (personagens + regiões)
//	@Param			type	path	string	true	"Tipo de ranking"
//	@Tags			ranking
//	@Produce		json
//	@Success		200	{object}	dranking.Facets
//	@Router			/v1/ranking/{type}/facets [get]
func GetRankingFacets(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := dranking.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		facets, err := svc.Facets(r.Context(), rt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, facets)
	}
}

// GetRankingPlayersByCountry retorna contagem de players únicos por país.
//
//	@Summary		Players únicos por país (pro mapa mundi)
//	@Param			type	path	string	true	"Tipo de ranking"
//	@Tags			ranking
//	@Produce		json
//	@Success		200	{array}	dranking.CountryPlayerCount
//	@Router			/v1/ranking/{type}/players-by-country [get]
func GetRankingPlayersByCountry(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt := dranking.RankingType(chi.URLParam(r, "type"))
		if !isValidRankingType(rt) {
			http.Error(w, `{"error":"ranking_type inválido"}`, http.StatusBadRequest)
			return
		}
		out, err := svc.PlayersByCountry(r.Context(), rt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, out)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func isValidRankingType(rt dranking.RankingType) bool {
	for _, valid := range dranking.AllRankingTypes() {
		if rt == valid {
			return true
		}
	}
	return false
}
