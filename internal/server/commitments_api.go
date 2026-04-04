package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/arclighteng/fin-go/internal/models"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleAPICreateCommitment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		Direction     string `json:"direction"`
		Cadence       string `json:"cadence"`
		Source        string `json:"source"`
		Confirmed     *int   `json:"confirmed"`
		ExpectedCents *int64 `json:"expected_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	c := models.Commitment{
		Name:      name,
		Direction: req.Direction,
		Cadence:   req.Cadence,
		Source:    req.Source,
	}
	if c.Direction == "" {
		c.Direction = "expense"
	}
	if c.Cadence == "" {
		c.Cadence = "monthly"
	}
	if c.Source == "" {
		c.Source = "manual"
	}
	if req.Confirmed != nil && *req.Confirmed == 1 {
		c.Confirmed = true
	}
	c.ExpectedCents = req.ExpectedCents
	c.MerchantNorm = strings.ToLower(name)

	id, err := s.db.SaveCommitment(c)
	if err != nil {
		log.Printf("handleAPICreateCommitment: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": id})
}

func (s *Server) handleAPIUpdateCommitment(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(raw) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no fields to update"})
		return
	}

	if err := s.db.UpdateCommitment(id, raw); err != nil {
		log.Printf("handleAPIUpdateCommitment: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteCommitment(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	if err := s.db.DeleteCommitment(id); err != nil {
		log.Printf("handleAPIDeleteCommitment: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDismissDuplicate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Merchant string `json:"merchant"`
		Dismiss  bool   `json:"dismiss"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	merchant := strings.TrimSpace(strings.ToLower(req.Merchant))
	if merchant == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "merchant is required"})
		return
	}

	if !req.Dismiss {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dismiss must be true"})
		return
	}

	if err := s.db.DismissDuplicateGroup(merchant); err != nil {
		log.Printf("handleAPIDismissDuplicate: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
