package mailcloak

import (
	"bufio"
	"bytes"
	"net"
	"testing"

	"mailcloak/internal/mailcloak/testutil"
)

func TestSocketmapFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSocketmapFrame(&buf, "alias test@example.com"); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	payload, err := readSocketmapFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if payload != "alias test@example.com" {
		t.Fatalf("expected payload %q, got %q", "alias test@example.com", payload)
	}
}

func TestHandleSocketmapConn(t *testing.T) {
	cfg := &Config{}
	cfg.Policy.Domain = "example.com"

	db := testutil.NewSQLiteDB(t)
	defer db.Close()
	mailDB := &MailcloakDB{DB: db}
	testutil.InsertAlias(t, db, "alias@example.com", "alice", true)

	roundTrip := func(t *testing.T, payload string) string {
		client, server := net.Pipe()
		defer client.Close()

		done := make(chan struct{})
		go func() {
			handleSocketmapConn(server, cfg, mailDB)
			close(done)
		}()

		if err := writeSocketmapFrame(client, payload); err != nil {
			t.Fatalf("write request: %v", err)
		}
		resp, err := readSocketmapFrame(bufio.NewReader(client))
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		_ = client.Close()
		<-done
		return resp
	}

	cases := []struct {
		name    string
		payload string
		expect  string
	}{
		{name: "alias found", payload: "alias alias@example.com", expect: "OK alice@example.com"},
		{name: "other domain", payload: "alias other@other.com", expect: "NOTFOUND"},
		{name: "wrong map", payload: "virtual alias@example.com", expect: "NOTFOUND"},
		{name: "empty payload", payload: "", expect: "NOTFOUND"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.payload)
			if got != tc.expect {
				t.Fatalf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}
