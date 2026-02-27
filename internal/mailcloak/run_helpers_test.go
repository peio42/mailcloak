package mailcloak

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestServiceSetErrKeepsFirstError(t *testing.T) {
	s := &Service{}
	first := errors.New("first")
	second := errors.New("second")

	s.setErr(first)
	s.setErr(second)

	if got := s.Err(); !errors.Is(got, first) {
		t.Fatalf("expected first error to be kept, got %v", got)
	}
}

func TestIsExpectedServeErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !isExpectedServeErr(ctx, nil) {
		t.Fatal("nil error should be expected")
	}
	if isExpectedServeErr(ctx, errors.New("boom")) {
		t.Fatal("regular error should not be expected while context is active")
	}

	cancel()
	if !isExpectedServeErr(ctx, errors.New("boom")) {
		t.Fatal("any error should be expected once context is cancelled")
	}

	ctx2 := context.Background()
	if !isExpectedServeErr(ctx2, net.ErrClosed) {
		t.Fatal("net.ErrClosed should be expected")
	}
}
