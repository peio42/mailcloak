package mailcloak

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type AdminHTTPOptions struct {
	Token      string
	ConfigPath string
	DBPath     string
}

func NewAdminHTTPHandler(db *MailcloakDB, opts AdminHTTPOptions) http.Handler {
	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = DefaultDBPath
	}
	h := &adminHTTPHandler{
		db:         db,
		token:      strings.TrimSpace(opts.Token),
		configPath: configPath,
		dbPath:     dbPath,
	}
	return h
}

type adminHTTPHandler struct {
	db         *MailcloakDB
	token      string
	configPath string
	dbPath     string
}

type apiError struct {
	Error string `json:"error"`
}

func (h *adminHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if serveAdminUI(w, r) {
		return
	}
	if r.URL.Path == "/api/health" {
		h.handleHealth(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.authorized(r) {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch {
	case r.URL.Path == "/api/setup/status":
		h.handleSetupStatus(w, r)
	case r.URL.Path == "/api/setup/validate":
		h.handleSetupValidate(w, r)
	case r.URL.Path == "/api/setup/apply":
		h.handleSetupApply(w, r)
	case r.URL.Path == "/api/idp/test":
		h.handleIDPTest(w, r)
	case r.URL.Path == "/api/domains":
		h.handleDomains(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/domains/"):
		h.handleDomain(w, r)
	case r.URL.Path == "/api/aliases":
		h.handleAliases(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/aliases/"):
		h.handleAlias(w, r)
	case r.URL.Path == "/api/apps":
		h.handleApps(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/apps/"):
		h.handleAppPath(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "not found")
	}
}

type setupConfigRequest struct {
	Config Config `json:"config"`
}

func (h *adminHTTPHandler) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, GetSetupStatus(h.configPath, h.dbPath))
}

func (h *adminHTTPHandler) handleSetupValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req setupConfigRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeAPIError(w, statusForDecodeError(err), err.Error())
		return
	}
	cfg := req.Config
	if cfg.SQLite.Path == "" {
		cfg.SQLite.Path = h.dbPath
	}
	if err := ValidateConfig(&cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"valid": true,
	})
}

func (h *adminHTTPHandler) handleSetupApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req SetupApplyRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeAPIError(w, statusForDecodeError(err), err.Error())
		return
	}
	if req.Config.SQLite.Path != "" && req.Config.SQLite.Path != h.dbPath {
		writeAPIError(w, http.StatusBadRequest, "config.sqlite.path must match the admin server db path")
		return
	}
	result, err := ApplySetup(r.Context(), h.configPath, h.dbPath, req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *adminHTTPHandler) handleIDPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req setupConfigRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeAPIError(w, statusForDecodeError(err), err.Error())
		return
	}
	cfg := req.Config
	if cfg.SQLite.Path == "" {
		cfg.SQLite.Path = h.dbPath
	}
	result, err := TestIdentityProvider(r.Context(), &cfg)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *adminHTTPHandler) authorized(r *http.Request) bool {
	if h.token == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+h.token
}

func (h *adminHTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *adminHTTPHandler) handleDomains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		domains, err := h.db.ListDomains(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if domains == nil {
			domains = []Domain{}
		}
		writeJSON(w, http.StatusOK, domains)
	case http.MethodPost:
		var req struct {
			DomainName string `json:"domain_name"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if err := h.db.UpsertDomain(r.Context(), req.DomainName); err != nil {
			writeAPIError(w, statusForAdminError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleDomain(w http.ResponseWriter, r *http.Request) {
	domainName, ok := pathValue(r.URL.Path, "/api/domains/")
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if req.Enabled == nil {
			writeAPIError(w, http.StatusBadRequest, "missing enabled")
			return
		}
		if err := h.db.SetDomainEnabled(r.Context(), domainName, *req.Enabled); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := h.db.DeleteDomain(r.Context(), domainName); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		aliases, err := h.db.ListAliases(r.Context(), r.URL.Query().Get("user"))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if aliases == nil {
			aliases = []Alias{}
		}
		writeJSON(w, http.StatusOK, aliases)
	case http.MethodPost:
		var req struct {
			AliasEmail string `json:"alias_email"`
			TargetUser string `json:"target_user"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if err := h.db.UpsertAlias(r.Context(), req.AliasEmail, req.TargetUser); err != nil {
			writeAPIError(w, statusForAdminError(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleAlias(w http.ResponseWriter, r *http.Request) {
	aliasEmail, ok := pathValue(r.URL.Path, "/api/aliases/")
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if req.Enabled == nil {
			writeAPIError(w, http.StatusBadRequest, "missing enabled")
			return
		}
		if err := h.db.SetAliasEnabled(r.Context(), aliasEmail, *req.Enabled); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := h.db.DeleteAlias(r.Context(), aliasEmail); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apps, err := h.db.ListApps(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if apps == nil {
			apps = []App{}
		}
		writeJSON(w, http.StatusOK, apps)
	case http.MethodPost:
		var req struct {
			AppID    string `json:"app_id"`
			Password string `json:"password"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if err := h.db.UpsertAppPassword(r.Context(), req.AppID, req.Password); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleAppPath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/apps/")
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		appID, err := url.PathUnescape(parts[0])
		if err != nil || appID == "" {
			writeAPIError(w, http.StatusNotFound, "not found")
			return
		}
		h.handleApp(w, r, appID)
		return
	}
	if len(parts) == 2 && parts[1] == "senders" {
		appID, err := url.PathUnescape(parts[0])
		if err != nil || appID == "" {
			writeAPIError(w, http.StatusNotFound, "not found")
			return
		}
		h.handleAppSenders(w, r, appID)
		return
	}
	if len(parts) == 3 && parts[1] == "senders" {
		appID, appErr := url.PathUnescape(parts[0])
		fromAddr, addrErr := url.PathUnescape(parts[2])
		if appErr != nil || addrErr != nil || appID == "" || fromAddr == "" {
			writeAPIError(w, http.StatusNotFound, "not found")
			return
		}
		h.handleAppSender(w, r, appID, fromAddr)
		return
	}
	writeAPIError(w, http.StatusNotFound, "not found")
}

func (h *adminHTTPHandler) handleApp(w http.ResponseWriter, r *http.Request, appID string) {
	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if req.Enabled == nil {
			writeAPIError(w, http.StatusBadRequest, "missing enabled")
			return
		}
		if err := h.db.SetAppEnabled(r.Context(), appID, *req.Enabled); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := h.db.DeleteApp(r.Context(), appID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleAppSenders(w http.ResponseWriter, r *http.Request, appID string) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			FromAddr string `json:"from_addr"`
		}
		if err := decodeJSONBody(w, r, &req); err != nil {
			writeAPIError(w, statusForDecodeError(err), err.Error())
			return
		}
		if err := h.db.AllowAppSender(r.Context(), appID, req.FromAddr); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *adminHTTPHandler) handleAppSender(w http.ResponseWriter, r *http.Request, appID, fromAddr string) {
	switch r.Method {
	case http.MethodDelete:
		if err := h.db.DeleteAppSender(r.Context(), appID, fromAddr); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func pathValue(path, prefix string) (string, bool) {
	value := strings.TrimPrefix(path, prefix)
	if value == "" || strings.Contains(value, "/") {
		return "", false
	}
	value, err := url.PathUnescape(value)
	if err != nil {
		return "", false
	}
	return value, true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return errMissingJSONBody
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("invalid json body: multiple values")
	}
	return nil
}

var errMissingJSONBody = errors.New("missing json body")

func statusForDecodeError(err error) int {
	if errors.Is(err, errMissingJSONBody) {
		return http.StatusBadRequest
	}
	return http.StatusBadRequest
}

func statusForAdminError(err error) int {
	if errors.Is(err, ErrAlreadyExists) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message})
}
