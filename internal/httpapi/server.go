package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/auth"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/service"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type Server struct {
	Store   *store.DB
	Auth    auth.Service
	Service service.Service
	IDs     idgen.Generator
	Logger  *slog.Logger
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("POST /v1/login", s.login)
	middleware := middleware{Auth: s.Auth, IDs: s.IDs, Logger: s.Logger}
	protected := http.NewServeMux()
	protected.HandleFunc("POST /v1/logout", s.logout)
	protected.HandleFunc("POST /v1/applications", s.submitApplication)
	protected.HandleFunc("GET /v1/applications", s.listApplications)
	protected.HandleFunc("GET /v1/applications/{id}", s.getApplication)
	protected.HandleFunc("POST /v1/applications/{id}/qualification", s.qualifyApplication)
	protected.HandleFunc("POST /v1/applications/{id}/plan", s.approvePlan)
	protected.HandleFunc("POST /v1/applications/{id}/allocation", s.allocate)
	protected.HandleFunc("POST /v1/applications/{id}/start", s.startTrial)
	protected.HandleFunc("POST /v1/applications/{id}/observation-batches", s.createBatch)
	protected.HandleFunc("POST /v1/applications/{id}/lock", s.lockApplication)
	protected.HandleFunc("POST /v1/observation-batches/{id}/observations", s.recordObservation)
	protected.HandleFunc("POST /v1/observation-batches/{id}/lock", s.lockBatch)
	protected.HandleFunc("POST /v1/applications/{id}/reviews", s.submitReview)
	protected.HandleFunc("POST /v1/applications/{id}/conclusions", s.draftConclusion)
	protected.HandleFunc("POST /v1/conclusions/{id}/publish", s.publishConclusion)
	protected.HandleFunc("POST /v1/adoptions", s.adopt)
	protected.HandleFunc("POST /v1/adoptions/{id}/revoke", s.revokeAdoption)
	protected.HandleFunc("GET /v1/audit/{object_type}/{object_id}", s.listAudit)
	mux.Handle("/v1/", middleware.authenticate(protected))
	return middleware.request(mux)
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(r.Context(), w, fmt.Errorf("decode request: %w: %w", err, apperror.ErrValidation))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(r.Context(), w, fmt.Errorf("request contains multiple JSON values: %w", apperror.ErrValidation))
		return false
	}
	return true
}

func currentPrincipal(w http.ResponseWriter, r *http.Request) (domain.Principal, bool) {
	principal, ok := principalFrom(r.Context())
	if !ok {
		writeError(r.Context(), w, apperror.ErrUnauthenticated)
	}
	return principal, ok
}

func pageFrom(r *http.Request) store.Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return store.Page{Limit: limit, Offset: offset}.Normalize()
}

func pathID(r *http.Request) string { return strings.TrimSpace(r.PathValue("id")) }
