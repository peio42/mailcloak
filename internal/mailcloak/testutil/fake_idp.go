package testutil

import (
	"context"
	"strings"
)

type FakeIdentityResolver struct {
	EmailByUser        map[string]string
	EmailExistsSet     map[string]bool
	EmailByUsernameErr error
	EmailExistsErr     error
}

func (f *FakeIdentityResolver) EmailByUsername(ctx context.Context, username string) (string, bool, error) {
	if f.EmailByUsernameErr != nil {
		return "", false, f.EmailByUsernameErr
	}
	if f.EmailByUser == nil {
		return "", false, nil
	}
	email, ok := f.EmailByUser[strings.ToLower(username)]
	if !ok {
		return "", false, nil
	}
	return strings.ToLower(email), true, nil
}

func (f *FakeIdentityResolver) EmailExists(ctx context.Context, email string) (bool, error) {
	if f.EmailExistsErr != nil {
		return false, f.EmailExistsErr
	}
	if f.EmailExistsSet == nil {
		return false, nil
	}
	return f.EmailExistsSet[strings.ToLower(email)], nil
}
