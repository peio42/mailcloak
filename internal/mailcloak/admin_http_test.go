package mailcloak

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminHTTPHealthAndAuth(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{Token: "secret"})

	rr := adminHTTPRequest(t, handler, http.MethodGet, "/api/health", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodGet, "/api/domains", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth domains status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodGet, "/api/domains", "", "secret")
	if rr.Code != http.StatusOK {
		t.Fatalf("auth domains status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminHTTPDomainsAndAliases(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{})

	rr := adminHTTPRequest(t, handler, http.MethodPost, "/api/domains", `{"domain_name":"Example.COM"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("create domain status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/aliases", `{"alias_email":"Alias@Example.COM","target_user":"alice"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("create alias status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodGet, "/api/aliases?user=alice", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list aliases status = %d body=%s", rr.Code, rr.Body.String())
	}
	var aliases []Alias
	if err := json.Unmarshal(rr.Body.Bytes(), &aliases); err != nil {
		t.Fatalf("decode aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].AliasEmail != "alias@example.com" || aliases[0].TargetUser != "alice" {
		t.Fatalf("unexpected aliases: %#v", aliases)
	}

	rr = adminHTTPRequest(t, handler, http.MethodPatch, "/api/aliases/"+url.PathEscape("alias@example.com"), `{"enabled":false}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("disable alias status = %d body=%s", rr.Code, rr.Body.String())
	}
	if _, ok, err := db.AliasOwner("alias@example.com"); err != nil || ok {
		t.Fatalf("alias should be disabled, ok=%v err=%v", ok, err)
	}
}

func TestAdminHTTPAppsAndSenders(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{})

	rr := adminHTTPRequest(t, handler, http.MethodPost, "/api/domains", `{"domain_name":"example.com"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("create domain status = %d body=%s", rr.Code, rr.Body.String())
	}
	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/apps", `{"app_id":"app1","password":"password"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("create app status = %d body=%s", rr.Code, rr.Body.String())
	}
	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/apps/app1/senders", `{"from_addr":"Sender@Example.COM"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("allow sender status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodGet, "/api/apps", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list apps status = %d body=%s", rr.Code, rr.Body.String())
	}
	var apps []App
	if err := json.Unmarshal(rr.Body.Bytes(), &apps); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(apps) != 1 || apps[0].AppID != "app1" || len(apps[0].Senders) != 1 || apps[0].Senders[0].FromAddr != "sender@example.com" {
		t.Fatalf("unexpected apps: %#v", apps)
	}

	deletePath := path.Join("/api/apps", url.PathEscape("app1"), "senders", url.PathEscape("sender@example.com"))
	rr = adminHTTPRequest(t, handler, http.MethodDelete, deletePath, "", "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete sender status = %d body=%s", rr.Code, rr.Body.String())
	}
	if allowed, err := db.AppFromAllowed("app1", "sender@example.com"); err != nil || allowed {
		t.Fatalf("sender should be removed, allowed=%v err=%v", allowed, err)
	}
}

func TestAdminHTTPRejectsBadJSON(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{})

	rr := adminHTTPRequest(t, handler, http.MethodPost, "/api/domains", `{"domain_name":"example.com"} {}`, "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid json body") {
		t.Fatalf("unexpected bad json response: %s", rr.Body.String())
	}
}

func TestAdminHTTPRejectsDuplicateDomainAndAlias(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{})

	rr := adminHTTPRequest(t, handler, http.MethodPost, "/api/domains", `{"domain_name":"example.com"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("create domain status = %d body=%s", rr.Code, rr.Body.String())
	}
	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/domains", `{"domain_name":"Example.COM"}`, "")
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate domain status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/aliases", `{"alias_email":"alias@example.com","target_user":"alice"}`, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("create alias status = %d body=%s", rr.Code, rr.Body.String())
	}
	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/aliases", `{"alias_email":"Alias@Example.COM","target_user":"bob"}`, "")
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate alias status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminHTTPSetupAndIDPTest(t *testing.T) {
	cfg, closeServer := testKeycloakConfig(t)
	defer closeServer()

	dir := t.TempDir()
	cfg.SQLite.Path = filepath.Join(dir, "state.db")
	configPath := filepath.Join(dir, "config.yaml")

	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{
		ConfigPath: configPath,
		DBPath:     cfg.SQLite.Path,
	})

	body := jsonBody(t, setupConfigRequest{Config: cfg})
	rr := adminHTTPRequest(t, handler, http.MethodPost, "/api/setup/validate", body, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("validate setup status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/idp/test", body, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("idp test status = %d body=%s", rr.Code, rr.Body.String())
	}
	var idpResult IDPTestResult
	if err := json.Unmarshal(rr.Body.Bytes(), &idpResult); err != nil {
		t.Fatalf("decode idp result: %v", err)
	}
	if !idpResult.OK || idpResult.Provider != "keycloak" {
		t.Fatalf("unexpected idp result: %#v", idpResult)
	}

	applyBody := jsonBody(t, SetupApplyRequest{
		Config:  cfg,
		InitDB:  true,
		TestIDP: true,
	})
	rr = adminHTTPRequest(t, handler, http.MethodPost, "/api/setup/apply", applyBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("apply setup status = %d body=%s", rr.Code, rr.Body.String())
	}

	rr = adminHTTPRequest(t, handler, http.MethodGet, "/api/setup/status", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("setup status code = %d body=%s", rr.Code, rr.Body.String())
	}
	var status SetupStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if !status.ConfigExists || !status.ConfigValid || !status.ConfigWritable || !status.DBExists || !status.DBInitialized || !status.DBWritable {
		t.Fatalf("unexpected setup status: %#v", status)
	}
}

func adminHTTPRequest(t *testing.T, handler http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func jsonBody(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	return string(body)
}
