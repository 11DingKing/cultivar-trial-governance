package httpapi

import (
	"net/http"
)

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "database": "available"})
}

func (s Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := s.Auth.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		writeError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s Server) logout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r.Header.Get("Authorization"))
	if err := s.Auth.Logout(r.Context(), token); err != nil {
		writeError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
