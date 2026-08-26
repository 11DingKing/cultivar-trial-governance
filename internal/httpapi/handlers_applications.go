package httpapi

import (
	"fmt"
	"net/http"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/service"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

func (s Server) submitApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.SubmitApplicationInput
	if !decode(w, r, &input) {
		return
	}
	input.IdempotencyKey = r.Header.Get("Idempotency-Key")
	result, err := s.Service.SubmitApplication(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s Server) listApplications(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	filter := store.ApplicationFilter{Region: principal.Region, Page: pageFrom(r)}
	if principal.Role == domain.RoleBreeder {
		filter.InstitutionID = principal.InstitutionID
	}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = domain.ApplicationStatus(status)
	}
	values, total, err := s.Store.ListApplications(r.Context(), filter)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values, "total": total, "limit": filter.Page.Normalize().Limit, "offset": filter.Page.Normalize().Offset})
}

func (s Server) getApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	value, err := s.Store.GetApplication(r.Context(), nil, pathID(r))
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	if err := principal.RequireRegion(value.Region); err != nil {
		writeError(r.Context(), w, err)
		return
	}
	if principal.Role == domain.RoleBreeder && principal.InstitutionID != value.ApplicantInstitutionID {
		writeError(r.Context(), w, fmt.Errorf("institution isolation: %w", apperror.ErrForbidden))
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s Server) qualifyApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.QualificationInput
	if !decode(w, r, &input) {
		return
	}
	input.ApplicationID = pathID(r)
	value, err := s.Service.QualifyApplication(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s Server) approvePlan(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.ApprovePlanInput
	if !decode(w, r, &input) {
		return
	}
	input.ApplicationID = pathID(r)
	value, err := s.Service.ApprovePlan(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) allocate(w http.ResponseWriter, r *http.Request) {
	principal, ok := currentPrincipal(w, r)
	if !ok {
		return
	}
	var input service.AllocateInput
	if !decode(w, r, &input) {
		return
	}
	input.ApplicationID = pathID(r)
	value, err := s.Service.AllocateResources(r.Context(), principal, input)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (s Server) startTrial(w http.ResponseWriter, r *http.Request) {
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
	value, err := s.Service.StartTrial(r.Context(), principal, pathID(r), input.PolicyRef)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
