package mailcloak

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminUIServesEmbeddedIndexAndAssets(t *testing.T) {
	db := newAdminTestDB(t)
	defer db.Close()
	handler := NewAdminHTTPHandler(db, AdminHTTPOptions{Token: "secret"})

	rr := adminHTTPRequest(t, handler, http.MethodGet, "/", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("index status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Mailcloak Admin") {
		t.Fatalf("index did not contain app marker")
	}
	if !strings.Contains(rr.Body.String(), `id="tokenPanel"`) || !strings.Contains(rr.Body.String(), `data-view="domains" disabled`) {
		t.Fatalf("index did not contain expected auth-gated UI controls")
	}

	rr = adminHTTPRequest(t, handler, http.MethodGet, "/assets/app.js", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("asset status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "apiFetch") {
		t.Fatalf("asset did not contain expected script")
	}
}
