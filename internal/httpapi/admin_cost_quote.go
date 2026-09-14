package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/providers"
)

func (s *Server) adminAccountCostQuote(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid account id")
		return
	}
	var request accounts.CreativeFabricaQuoteRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid cost quote request")
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	account, err := s.Store.GetAccount(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	if account.ProviderID != providers.CreativeFabrica {
		writeError(w, http.StatusBadRequest, "provider_mismatch", "cost quotes require a Creative Fabrica account")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	quote, err := s.Accounts.QuoteCreativeFabricaCost(ctx, account, request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cost_quote_failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, quote)
}
