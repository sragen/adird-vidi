package passenger

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"adird.id/vidi/internal/auth"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// Routes returns chi router. All routes require passenger JWT.
func (h *Handler) Routes(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()
	r.Use(auth.Middleware(authSvc))
	r.Use(auth.RequireRole(auth.RoleUser, authSvc))

	r.Get("/profile", h.getProfile)
	r.Put("/profile", h.updateProfile)
	return r
}

// ─── GET /api/v1/passenger/profile ───────────────────────────────

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	u, err := h.repo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			jsonError(w, "user not found", http.StatusNotFound)
		} else {
			log.Error().Err(err).Msg("passenger getProfile")
			jsonError(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	jsonOK(w, u)
}

// ─── PUT /api/v1/passenger/profile ───────────────────────────────
// Body: {"name":"Adi Kurniawan"}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	u, err := h.repo.UpdateName(r.Context(), claims.UserID, req.Name)
	if err != nil {
		log.Error().Err(err).Msg("passenger updateProfile")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonOK(w, u)
}

// ─── helpers ─────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
