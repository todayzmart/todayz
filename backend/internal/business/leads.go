package business

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	publicapi "github.com/todayz/backend/gen/api/public"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// leadDedupeWindow is how long the same details are stored once (the
// contract's text for POST /v1/public/leads).
//
// TODO(lead): 01 does not list it as a setting; if it should be one, it
// moves to platform_settings.
const leadDedupeWindow = 24 * time.Hour

var errNoHuman = errors.New("business: the Turnstile check is not configured")

// leadText lower-cases and collapses spaces, so "Lakshmi  Traders" and
// "lakshmi traders" are the same details.
func leadText(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// leadHash is the keyed hash the 24-hour dedupe compares (no lead detail is
// stored in clear in it).
func leadHash(h *pii.Hasher, e164, name, shop, city string) []byte {
	return h.Sum("lead:" + e164 + "\x00" + leadText(name) + "\x00" + leadText(shop) + "\x00" + leadText(city))
}

// createLead stores the site's lead form after the Turnstile check. A repeat
// of the same details inside the window is accepted and not stored again.
func (s *Service) createLead(ctx context.Context, body *publicapi.LeadCreate) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.human == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errNoHuman)
	}
	if body == nil {
		return problem.New(genproblems.BadRequest, "")
	}
	// Turnstile first (the contract): a bot learns nothing about the fields.
	passed, err := s.human.Verify(ctx, body.TurnstileToken, reqctx.ClientIP(ctx))
	if err != nil {
		return problem.Wrap(genproblems.ServiceUnavailable, fmt.Errorf("business: turnstile: %w", err))
	}
	if !passed {
		return problem.New(genproblems.Forbidden, "")
	}
	var v invalid
	v.text("body.name", &body.Name)
	v.text("body.shopName", &body.ShopName)
	v.text("body.city", &body.City)
	v.text("body.source", &body.Source)
	e164, ok := pii.NormalizePhone(body.Phone)
	if !ok {
		v.add("body.phone", "must be an Indian mobile number")
	}
	if err := v.err(); err != nil {
		return err
	}
	hash := leadHash(s.hash, e164, body.Name, body.ShopName, body.City)
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.LockKey(ctx, "lead:"+hex.EncodeToString(hash)); err != nil {
			return err
		}
		now := s.clock.Now()
		seen, err := q.RecentLeadExists(ctx, businessdb.RecentLeadExistsParams{DetailsHash: hash, Since: now.Add(-leadDedupeWindow)})
		if err != nil {
			return err
		}
		if seen {
			return nil
		}
		enc, err := s.seal.Seal(FieldPhone, []byte(e164))
		if err != nil {
			return fmt.Errorf("business: seal lead phone: %w", err)
		}
		return q.InsertLead(ctx, businessdb.InsertLeadParams{
			ID: ids.UUID(ids.New(now)), Name: body.Name, PhoneEnc: enc, ShopName: body.ShopName,
			City: body.City, Source: body.Source, DetailsHash: hash, Now: now,
		})
	})
}
