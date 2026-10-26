package assist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/ids"
)

type allow map[string]bool

func (a allow) Check(_ context.Context, _ authn.Principal, action string, _ authz.Scope) (authz.Decision, error) {
	if a[action] {
		return authz.Decision{}, nil
	}
	return authz.Decision{}, errors.New("denied")
}

func TestOwn(t *testing.T) {
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	me, other, store := ids.New(at), ids.New(at), ids.New(at)
	assert.NoError(t, Own(me)(drafts.Draft{Actor: me}))
	assert.Error(t, Own(me)(drafts.Draft{Actor: other}), "another admin's draft")
	assert.Error(t, Own(me)(drafts.Draft{Actor: me, Store: store}), "a store's draft is never an admin's")
}

func TestCan(t *testing.T) {
	ctx := context.Background()
	p := authn.Principal{App: authn.AppAdmin}
	assert.False(t, Can(ctx, nil, p, "admin.catalogue.manage"), "no authorizer allows nothing")
	az := allow{"admin.catalogue.review": true}
	assert.True(t, Can(ctx, az, p, "admin.catalogue.review"))
	assert.False(t, Can(ctx, az, p, "admin.catalogue.manage"))
}

func TestCaller(t *testing.T) {
	_, err := Caller(context.Background())
	assert.Error(t, err, "no principal")
}
