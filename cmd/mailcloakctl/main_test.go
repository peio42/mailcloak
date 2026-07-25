package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunManagesDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	commands := [][]string{
		{"--db", dbPath, "init"},
		{"--db", dbPath, "domains", "add", "example.com"},
		{"--db", dbPath, "aliases", "add", "alias@example.com", "alice"},
		{"--db", dbPath, "apps", "add", "app1", "password"},
		{"--db", dbPath, "apps", "allow", "app1", "app1@example.com"},
	}
	for _, cmd := range commands {
		if err := run(cmd, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v stderr=%s", cmd, err, stderr.String())
		}
	}

	stdout.Reset()
	if err := run([]string{"--db", dbPath, "aliases", "list", "--user", "alice"}, &stdout, &stderr); err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if got := stdout.String(); got != "alias@example.com\talice\tenabled\n" {
		t.Fatalf("unexpected aliases output: %q", got)
	}

	stdout.Reset()
	if err := run([]string{"--db", dbPath, "apps", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("list apps: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "app1\tenabled\t") || !strings.Contains(got, "\t\tapp1@example.com\n") {
		t.Fatalf("unexpected apps output: %q", got)
	}
}

func TestParseAppAddArgsAllowsPositionalPassword(t *testing.T) {
	appID, password, err := parseAppAddArgs([]string{"app1", "password"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parse app args: %v", err)
	}
	if appID != "app1" || password != "password" {
		t.Fatalf("unexpected parsed values: appID=%q password=%q", appID, password)
	}
}
