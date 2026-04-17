package developer

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// fakeAuthCodeRepo is an in-memory AuthorizationCodeRepository for handler
// and flow tests. Not safe for concurrent use (tests are sequential).
type fakeAuthCodeRepo struct {
	byCode map[string]*AuthorizationCode
	byID   map[string]*AuthorizationCode
}

func newFakeAuthCodeRepo() *fakeAuthCodeRepo {
	return &fakeAuthCodeRepo{
		byCode: map[string]*AuthorizationCode{},
		byID:   map[string]*AuthorizationCode{},
	}
}

func (f *fakeAuthCodeRepo) Create(_ context.Context, c *AuthorizationCode) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	cp := *c
	f.byCode[c.Code] = &cp
	f.byID[c.ID] = &cp
	return nil
}

func (f *fakeAuthCodeRepo) GetByCode(_ context.Context, code string) (*AuthorizationCode, error) {
	c, ok := f.byCode[code]
	if !ok {
		return nil, ErrAuthorizationCodeNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeAuthCodeRepo) MarkConsumed(_ context.Context, id string, at time.Time) error {
	c, ok := f.byID[id]
	if !ok {
		return ErrAuthorizationCodeNotFound
	}
	if c.ConsumedAt != nil {
		return ErrAuthorizationCodeConsumed
	}
	c.ConsumedAt = &at
	return nil
}

var _ AuthorizationCodeRepository = (*fakeAuthCodeRepo)(nil)

// fakeOAuthTokenRepo is an in-memory OAuthTokenRepository for tests.
type fakeOAuthTokenRepo struct {
	byID     map[string]*OAuthToken
	byPrefix map[string][]*OAuthToken
}

func newFakeOAuthTokenRepo() *fakeOAuthTokenRepo {
	return &fakeOAuthTokenRepo{
		byID:     map[string]*OAuthToken{},
		byPrefix: map[string][]*OAuthToken{},
	}
}

func (f *fakeOAuthTokenRepo) Create(_ context.Context, t *OAuthToken) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	cp := *t
	f.byID[t.ID] = &cp
	f.byPrefix[t.TokenPrefix] = append(f.byPrefix[t.TokenPrefix], &cp)
	return nil
}

func (f *fakeOAuthTokenRepo) GetByPrefix(_ context.Context, prefix, tokenType string) ([]*OAuthToken, error) {
	rows := f.byPrefix[prefix]
	var out []*OAuthToken
	for _, r := range rows {
		if r.TokenType == tokenType {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeOAuthTokenRepo) Revoke(_ context.Context, id string, at time.Time) error {
	t, ok := f.byID[id]
	if !ok {
		return ErrTokenNotFound
	}
	if t.RevokedAt != nil {
		return ErrTokenNotFound
	}
	t.RevokedAt = &at
	return nil
}

var _ OAuthTokenRepository = (*fakeOAuthTokenRepo)(nil)
