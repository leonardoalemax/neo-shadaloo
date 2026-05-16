package handlers

import (
	"encoding/json"
	"net/http"

	app "neo-shadaloo/internal/application/league"
	dleague "neo-shadaloo/internal/domain/league"
)

// referências usadas pelo swag pra gerar schemas
var _ = dleague.SyncMeta{}
var _ = dleague.CountryPlayerCount{}

// PostLeagueSync dispara o sync do ranking de league em background.
//
//	@Summary		Dispara sync do ranking league_point
//	@Description	Faz o crawl de todas as páginas e upserta os players na tabela league_player (não apaga dados antigos).
//	@Tags			league
//	@Produce		json
//	@Success		202	{object}	map[string]string
//	@Router			/v1/league/sync [post]
func PostLeagueSync(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc.TriggerSync()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "league sync iniciado em background",
		})
	}
}

// GetLeagueStatus devolve o meta do sync atual.
//
//	@Summary		Status do sync de league
//	@Tags			league
//	@Produce		json
//	@Success		200	{object}	dleague.SyncMeta
//	@Router			/v1/league/status [get]
func GetLeagueStatus(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meta, err := svc.GetMeta(r.Context())
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"meta":    meta,
			"running": svc.IsRunning(),
		})
	}
}

// GetLeaguePlayersByCountry devolve a contagem de players únicos por país.
//
//	@Summary		Players por país (league)
//	@Tags			league
//	@Produce		json
//	@Success		200	{array}	dleague.CountryPlayerCount
//	@Router			/v1/league/players-by-country [get]
func GetLeaguePlayersByCountry(svc *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.PlayersByCountry(r.Context())
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
