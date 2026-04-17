package developer

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
)

// fakeApplicationRepo is an in-memory ApplicationRepository used by handler
// unit tests in this package. Safe to pass by pointer; not safe for
// concurrent use (tests are sequential).
type fakeApplicationRepo struct {
	byID       map[string]*Application
	byClientID map[string]*Application
}

func newFakeApplicationRepo() *fakeApplicationRepo {
	return &fakeApplicationRepo{
		byID:       map[string]*Application{},
		byClientID: map[string]*Application{},
	}
}

func (f *fakeApplicationRepo) Create(_ context.Context, app *Application) error {
	if app.ID == "" {
		app.ID = uuid.NewString()
	}
	if app.CreatedAt.IsZero() {
		app.CreatedAt = time.Now()
	}
	if app.UpdatedAt.IsZero() {
		app.UpdatedAt = app.CreatedAt
	}
	if app.RedirectURIs == nil {
		app.RedirectURIs = []string{}
	}
	if app.Scopes == nil {
		app.Scopes = []string{}
	}
	cp := *app
	f.byID[app.ID] = &cp
	f.byClientID[app.ClientID] = &cp
	return nil
}

func (f *fakeApplicationRepo) GetByID(_ context.Context, id string) (*Application, error) {
	a, ok := f.byID[id]
	if !ok {
		return nil, ErrApplicationNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeApplicationRepo) GetByClientID(_ context.Context, clientID string) (*Application, error) {
	a, ok := f.byClientID[clientID]
	if !ok {
		return nil, ErrApplicationNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeApplicationRepo) ListByUser(_ context.Context, userID string) ([]*Application, error) {
	var out []*Application
	for _, a := range f.byID {
		if a.CreatedBy == userID {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeApplicationRepo) Delete(_ context.Context, id string) error {
	a, ok := f.byID[id]
	if !ok {
		return ErrApplicationNotFound
	}
	delete(f.byID, id)
	delete(f.byClientID, a.ClientID)
	return nil
}

var _ ApplicationRepository = (*fakeApplicationRepo)(nil)
