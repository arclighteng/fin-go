package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/arclighteng/fin-go/internal/categorize"
	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/arclighteng/fin-go/internal/models"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleAPICategories(w http.ResponseWriter, r *http.Request) {
	cats := make([]map[string]string, 0, len(categorize.Categories))
	for _, c := range categorize.Categories {
		cats = append(cats, map[string]string{
			"id":    c.ID,
			"name":  c.Name,
			"icon":  c.Icon,
			"color": c.Color,
		})
	}
	writeJSON(w, http.StatusOK, cats)
}

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing query parameter 'q'"})
		return
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	txns, err := s.db.SearchTransactions(q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, txns)
}

func (s *Server) handleAPIIncomeSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Merchant string `json:"merchant"`
		IsIncome bool   `json:"is_income"`
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

	if err := s.db.SaveIncomeSource(merchant, req.IsIncome); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIIncomeSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.db.GetIncomeSources()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleAPICategoryOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Merchant   string `json:"merchant"`
		CategoryID string `json:"category_id"`
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

	if req.CategoryID == "auto" {
		if err := s.db.DeleteCategoryOverride(merchant); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		if _, ok := categorize.Categories[req.CategoryID]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown category_id"})
			return
		}
		if err := s.db.SaveCategoryOverride(merchant, req.CategoryID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIAlertAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AlertKey string `json:"alert_key"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.AlertKey == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alert_key and action required"})
		return
	}

	aa := models.AlertAction{
		AlertKey: req.AlertKey,
		Action:   req.Action,
	}
	if err := s.db.SaveAlertAction(aa); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIGetAnnotations(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	note, tags, err := s.db.GetTransactionAnnotations(fp)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fingerprint": fp,
		"note":        note,
		"tags":        tags,
	})
}

func (s *Server) handleAPISaveNote(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := s.db.SaveTransactionNote(fp, req.Note); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteNote(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	if err := s.db.DeleteTransactionNote(fp); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIAddTag(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	var req struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	tag := strings.TrimSpace(strings.ToLower(req.Tag))
	if tag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag is required"})
		return
	}
	if err := s.db.AddTransactionTag(fp, tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteTag(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	tag := chi.URLParam(r, "tag")
	if err := s.db.DeleteTransactionTag(fp, tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIGetTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.db.GetAllTags()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleAPIBudgetTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := s.db.GetBudgetTargets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) handleAPISaveBudgetTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CategoryID        string `json:"category_id"`
		MonthlyTargetCents int64  `json:"monthly_target_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.CategoryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category_id required"})
		return
	}
	if err := s.db.SaveBudgetTarget(req.CategoryID, req.MonthlyTargetCents); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteBudgetTarget(w http.ResponseWriter, r *http.Request) {
	catID := chi.URLParam(r, "categoryID")
	if err := s.db.DeleteBudgetTarget(catID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPISimpleFinToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessURL string `json:"access_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.AccessURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access_url required"})
		return
	}
	if !strings.HasPrefix(req.AccessURL, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access_url must use HTTPS"})
		return
	}

	if err := credentials.SetSimpleFinURL(req.AccessURL); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

