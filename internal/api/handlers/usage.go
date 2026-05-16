package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	appusage "neo-shadaloo/internal/application/usage"
)

var yyyymmRe = regexp.MustCompile(`^\d{6}$`)

// GetUsageMonths godoc
//
//	@Summary		List available usage months
//	@Description	Returns YYYYMM periods that have cached usage data, sorted newest-first.
//	@Tags			usage
//	@Produce		json
//	@Success		200	{array}		string
//	@Failure		500	{string}	string	"internal server error"
//	@Router			/v1/usage/months [get]
func GetUsageMonths(svc *appusage.UsageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		months, err := svc.GetAvailableMonths(r.Context())
		if err != nil {
			log.Printf("[handler] GetUsageMonths error: %v", err)
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

// GetUsage godoc
//
//	@Summary		Get character usage rates for a given month
//	@Description	Returns cached character usage rates (usagerate_list) for the given YYYYMM period. Triggers a background sync if missing or stale (>24h).
//	@Tags			usage
//	@Produce		json
//	@Param			yyyymm	path		string				true	"Year-month (e.g. 202602)"
//	@Success		200		{object}	usage.UsageSnapshot
//	@Failure		400		{string}	string	"invalid yyyymm"
//	@Failure		500		{string}	string	"internal server error"
//	@Router			/v1/usage/{yyyymm} [get]
func GetUsage(svc *appusage.UsageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		yyyymm := chi.URLParam(r, "yyyymm")
		if !yyyymmRe.MatchString(yyyymm) {
			http.Error(w, "invalid yyyymm", http.StatusBadRequest)
			return
		}
		snap, err := svc.GetUsage(r.Context(), yyyymm)
		if err != nil {
			log.Printf("[handler] GetUsage error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(snap)
	}
}

// PostUsageSync godoc
//
//	@Summary		Force a usage data sync for a given month
//	@Description	Triggers an immediate background sync with the SF6 API for the given YYYYMM period. Returns 202 immediately.
//	@Tags			usage
//	@Produce		json
//	@Param			yyyymm	path		string			true	"Year-month (e.g. 202602)"
//	@Success		202		{object}	map[string]string
//	@Failure		400		{string}	string	"invalid yyyymm"
//	@Router			/v1/usage/{yyyymm}/sync [post]
func PostUsageSync(svc *appusage.UsageService) http.HandlerFunc {
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
