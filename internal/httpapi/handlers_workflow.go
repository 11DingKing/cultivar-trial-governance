package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/service"
)

func (s Server) createBatch(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.CreateBatchInput
	if !decode(w, r, &input) {
		return
	}
	input.ApplicationID = pathID(r)
	value, err := s.Service.CreateObservationBatch(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) recordObservation(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Metric     string    `json:"metric"`
		Value      float64   `json:"value"`
		Unit       string    `json:"unit"`
		ObservedAt time.Time `json:"observed_at"`
		Minimum    float64   `json:"minimum"`
		Maximum    float64   `json:"maximum"`
		PolicyRef  string    `json:"policy_ref"`
	}
	if !decode(w, r, &body) {
		return
	}
	input := service.RecordObservationInput{
		BatchID: pathID(r), Metric: body.Metric, Value: body.Value, Unit: body.Unit,
		ObservedAt: body.ObservedAt, PolicyRef: body.PolicyRef,
		Rule: domain.ObservationRule{Metric: body.Metric, Unit: body.Unit, Min: body.Minimum, Max: body.Maximum},
	}
	value, err := s.Service.RecordObservation(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) lockBatch(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input struct {
		PolicyRef string `json:"policy_ref"`
	}
	if !decode(w, r, &input) {
		return
	}
	value, err := s.Service.LockObservationBatch(r.Context(), principal, pathID(r), input.PolicyRef)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s Server) lockApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input struct {
		PolicyRef string `json:"policy_ref"`
	}
	if !decode(w, r, &input) {
		return
	}
	value, err := s.Service.LockApplicationData(r.Context(), principal, pathID(r), input.PolicyRef)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s Server) submitReview(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.SubmitReviewInput
	if !decode(w, r, &input) {
		return
	}
	input.ApplicationID = pathID(r)
	value, err := s.Service.SubmitReview(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) draftConclusion(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.DraftConclusionInput
	if !decode(w, r, &input) {
		return
	}
	input.ApplicationID = pathID(r)
	value, err := s.Service.DraftConclusion(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) publishConclusion(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	value, err := s.Service.PublishConclusion(r.Context(), principal, pathID(r))
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s Server) adopt(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.AdoptInput
	if !decode(w, r, &input) {
		return
	}
	value, err := s.Service.AdoptConclusion(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) revokeAdoption(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input struct {
		Reason    string `json:"reason"`
		PolicyRef string `json:"policy_ref"`
	}
	if !decode(w, r, &input) {
		return
	}
	value, err := s.Service.RevokeAdoption(r.Context(), principal, pathID(r), input.Reason, input.PolicyRef)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s Server) listAudit(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	if principal.Role != domain.RoleAdmin && principal.Role != domain.RoleReviewExpert {
		writeError(r.Context(), w, fmt.Errorf("audit access: %w", apperror.ErrForbidden))
		return
	}
	values, err := s.Store.ListAuditForObject(r.Context(), r.PathValue("object_type"), r.PathValue("object_id"), pageFrom(r))
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}
