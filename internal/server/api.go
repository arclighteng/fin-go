package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/arclighteng/fin-go/internal/categorize"
	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/arclighteng/fin-go/internal/dates"
	"github.com/arclighteng/fin-go/internal/db"
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

	var opts db.SearchOptions

	// days filter: cap at 3650, compute min date in user's timezone.
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			if d > 3650 {
				d = 3650
			}
			today := dates.Today(s.cfg.Timezone)
			minDate := today.AddDate(0, 0, -d)
			opts.MinDate = minDate.Format("2006-01-02")
		}
	}

	// accounts filter: repeated query param.
	if accts := r.URL.Query()["accounts"]; len(accts) > 0 {
		for _, a := range accts {
			if a = strings.TrimSpace(a); a != "" {
				opts.Accounts = append(opts.Accounts, a)
			}
		}
	}

	txns, err := s.db.SearchTransactions(q, limit, opts)
	if err != nil {
		log.Printf("handleAPISearch: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Map to the shape the frontend expects: {matches: [{date, amount_cents, merchant, description, account_name}]}
	matches := make([]map[string]any, 0, len(txns))
	for _, t := range txns {
		matches = append(matches, map[string]any{
			"date":         t.PostedAt.Format("2006-01-02"),
			"amount_cents": t.AmountCents,
			"merchant":     t.Merchant,
			"description":  t.Description,
			"account_name": t.AccountName,
			"fingerprint":  t.Fingerprint,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
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
		log.Printf("handleAPIIncomeSource: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIIncomeSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.db.GetIncomeSources()
	if err != nil {
		log.Printf("handleAPIIncomeSources: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
			log.Printf("handleAPICategoryOverride: DeleteCategoryOverride: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
	} else {
		if _, ok := categorize.Categories[req.CategoryID]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown category_id"})
			return
		}
		if err := s.db.SaveCategoryOverride(merchant, req.CategoryID); err != nil {
			log.Printf("handleAPICategoryOverride: SaveCategoryOverride: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		log.Printf("handleAPIAlertAction: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIGetAnnotations(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	note, tags, err := s.db.GetTransactionAnnotations(fp)
	if err != nil {
		log.Printf("handleAPIGetAnnotations: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		log.Printf("handleAPISaveNote: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteNote(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	if err := s.db.DeleteTransactionNote(fp); err != nil {
		log.Printf("handleAPIDeleteNote: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		log.Printf("handleAPIAddTag: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteTag(w http.ResponseWriter, r *http.Request) {
	fp := chi.URLParam(r, "fingerprint")
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag is required"})
		return
	}
	if err := s.db.DeleteTransactionTag(fp, tag); err != nil {
		log.Printf("handleAPIDeleteTag: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIGetTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.db.GetAllTags()
	if err != nil {
		log.Printf("handleAPIGetTags: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleAPIBudgetTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := s.db.GetBudgetTargets()
	if err != nil {
		log.Printf("handleAPIBudgetTargets: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) handleAPISaveBudgetTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CategoryID         string `json:"category_id"`
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
		log.Printf("handleAPISaveBudgetTarget: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPIDeleteBudgetTarget(w http.ResponseWriter, r *http.Request) {
	catID := chi.URLParam(r, "categoryID")
	if err := s.db.DeleteBudgetTarget(catID); err != nil {
		log.Printf("handleAPIDeleteBudgetTarget: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// privateIPBlocks lists CIDR ranges that must not be reachable via the
// SimpleFIN access URL. These cover loopback, private, and link-local ranges.
var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique-local
		"fe80::/10",      // IPv6 link-local
	} {
		_, block, _ := net.ParseCIDR(cidr)
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

// isPrivateIP reports whether ip falls within any private/loopback range.
func isPrivateIP(ip net.IP) bool {
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// validateSimpleFinURL checks that u is an HTTPS URL pointing at a host
// under simplefin.org and not at a private/loopback address.
func validateSimpleFinURL(raw string) string {
	if !strings.HasPrefix(raw, "https://") {
		return "access_url must use HTTPS"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "access_url is not a valid URL"
	}
	host := u.Hostname()

	// If the host is a bare IP address, reject it — SimpleFIN always uses a hostname.
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return "access_url points to a private/loopback address"
		}
		return "access_url must be a SimpleFIN hostname, not a bare IP"
	}

	// Resolve the hostname and check that none of the IPs are private.
	addrs, err := net.LookupHost(host)
	if err == nil {
		for _, addr := range addrs {
			if ip := net.ParseIP(addr); ip != nil && isPrivateIP(ip) {
				return "access_url resolves to a private/loopback address"
			}
		}
	}
	// Enforce that the hostname is under simplefin.org.
	if !strings.HasSuffix(host, ".simplefin.org") && host != "simplefin.org" {
		return "access_url hostname must end with .simplefin.org"
	}

	return ""
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
	if errMsg := validateSimpleFinURL(req.AccessURL); errMsg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": errMsg})
		return
	}

	if err := credentials.SetSimpleFinURL(req.AccessURL); err != nil {
		log.Printf("handleAPISimpleFinToken: SetSimpleFinURL: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
