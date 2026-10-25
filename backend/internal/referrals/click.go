package referrals

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// CookieName is the attribution cookie (D-32). It carries a click id, never
// a code or a person.
const CookieName = "__Secure-tz_ref"

// reCode is a referral code or store short code (no 0, O, 1, I or L).
var reCode = regexp.MustCompile(`^[A-HJKMNP-Z2-9]{6}$`)

// normCode upper-cases and trims a code; "" when it is not a code.
func normCode(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if !reCode.MatchString(c) {
		return ""
	}
	return c
}

// referrer is who a code or click names.
type referrer struct {
	Type      string // partyCustomer, partyStore, partyBusiness
	ID        ids.ID
	Code      string
	StandeeID ids.ID // a store's standee, from the click
	ClickID   ids.ID
	// For a store: its business and slug.
	BusinessID ids.ID
	Slug       string
}

// resolve looks a code up as the given party types, in order; ok false when
// none matches.
func (s *Service) resolve(ctx context.Context, code string, as ...string) (referrer, bool, error) {
	q := s.q(ctx)
	for _, t := range as {
		switch t {
		case partyCustomer:
			u, err := q.CustomerByCode(ctx, code)
			if err == nil {
				return referrer{Type: partyCustomer, ID: idOf(u), Code: code}, true, nil
			}
			if !db.IsNoRows(err) {
				return referrer{}, false, err
			}
		case partyBusiness:
			b, err := q.BusinessByCode(ctx, code)
			if err == nil {
				return referrer{Type: partyBusiness, ID: idOf(b), Code: code}, true, nil
			}
			if !db.IsNoRows(err) {
				return referrer{}, false, err
			}
		case partyStore:
			st, err := q.StoreByShortCode(ctx, code)
			if err == nil {
				return referrer{Type: partyStore, ID: idOf(st.ID), Code: code, BusinessID: idOf(st.BusinessID), Slug: st.Slug}, true, nil
			}
			if !db.IsNoRows(err) {
				return referrer{}, false, err
			}
		}
	}
	return referrer{}, false, nil
}

// Click is an open of GET /v1/public/ref/{code}.
type Click struct {
	Code      string
	Standee   string // ?s=
	Kind      string // ?k= (c, b, s) or ""
	Cookie    string // the current attribution cookie's value, if any
	UserAgent string
	IP        string
}

// Landing is where the browser goes and the cookie to set ("" keeps the
// current one).
type Landing struct {
	Location  string
	SetCookie string
}

// Open records a click and returns the redirect (D-32, D-26). An unknown code
// records nothing, sets no cookie and goes to the customer app.
func (s *Service) Open(ctx context.Context, c Click) (Landing, error) {
	home := Landing{Location: s.customerApp + "/"}
	if err := s.ready(ctx); err != nil {
		return Landing{}, err
	}
	code := normCode(c.Code)
	if code == "" {
		return home, nil
	}
	standee, standeeErr := ids.Parse(strings.TrimSpace(c.Standee))
	hasStandee := c.Standee != "" && standeeErr == nil
	var order []string
	switch c.Kind {
	case "c":
		order = []string{partyCustomer}
	case "b":
		order = []string{partyBusiness}
	case "s":
		order = []string{partyStore}
	default:
		if hasStandee {
			order = []string{partyStore, partyCustomer, partyBusiness}
		} else {
			// TODO(lead): codes are not unique across customers, businesses
			// and stores; an unmarked link tries them in this order.
			order = []string{partyCustomer, partyBusiness, partyStore}
		}
	}
	r, ok, err := s.resolve(ctx, code, order...)
	if err != nil {
		return Landing{}, err
	}
	if !ok {
		return home, nil
	}
	now := s.now()
	win, err := s.window(ctx)
	if err != nil {
		return Landing{}, err
	}
	click := referralsdb.InsertClickParams{
		ID: ids.UUID(s.ids.New(now)), Code: code, ReferrerType: r.Type, ReferrerID: ids.UUID(r.ID),
		Landing: "app", CreatedAt: now,
	}
	out := Landing{Location: s.customerApp + "/"}
	switch r.Type {
	case partyStore:
		click.Landing = "store"
		loc := s.customerApp + "/s/" + url.PathEscape(r.Slug)
		if hasStandee {
			// SC-ST-03: only a standee of this store is a scan. Another
			// store's or an unknown id still lands on the store, unattributed.
			mine, err := s.q(ctx).StoreStandee(ctx, referralsdb.StoreStandeeParams{ID: ids.UUID(standee), StoreID: ids.UUID(r.ID)})
			if err != nil {
				return Landing{}, err
			}
			if mine {
				click.StandeeID = ids.UUID(standee)
				loc += "?s=" + url.QueryEscape(standee.String())
			}
		}
		out.Location = loc
	case partyBusiness:
		out.Location = s.businessApp + "/"
	}
	if c.IP != "" {
		click.IpHash = s.hasher.IP(c.IP)
	}
	if ua := strings.TrimSpace(c.UserAgent); ua != "" {
		click.UaHash = s.hasher.Sum("ua:" + ua)
	}
	if err := s.q(ctx).InsertClick(ctx, click); err != nil {
		return Landing{}, err
	}
	if !s.cookieLive(ctx, c.Cookie, now, win) {
		out.SetCookie = (&http.Cookie{
			Name: CookieName, Value: idOf(click.ID).String(), Path: "/v1",
			MaxAge: int(win / time.Second), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		}).String()
	}
	return out, nil
}

// cookieLive reports whether the cookie names a click still inside the
// window (first touch: it is kept).
func (s *Service) cookieLive(ctx context.Context, v string, now time.Time, win time.Duration) bool {
	id, err := ids.Parse(strings.TrimSpace(v))
	if v == "" || err != nil {
		return false
	}
	cl, err := s.q(ctx).GetClick(ctx, ids.UUID(id))
	if err != nil {
		if !db.IsNoRows(err) {
			telemetry.Logger(ctx).WarnContext(ctx, "referrals: read attribution click", "err", err)
		}
		return false
	}
	return !now.After(cl.CreatedAt.Add(win))
}

// cookieClick is the click id a cookie holds; a missing or malformed cookie
// is no click.
func cookieClick(v string) (ids.ID, bool) {
	id, err := ids.Parse(strings.TrimSpace(v))
	return id, v != "" && err == nil
}

// clickReferrer reads the click a cookie names; ok false when there is none.
func (s *Service) clickReferrer(ctx context.Context, v string) (referrer, time.Time, bool, error) {
	id, ok := cookieClick(v)
	if !ok {
		return referrer{}, time.Time{}, false, nil
	}
	cl, err := s.q(ctx).GetClick(ctx, ids.UUID(id))
	if err != nil {
		if db.IsNoRows(err) {
			return referrer{}, time.Time{}, false, nil
		}
		return referrer{}, time.Time{}, false, err
	}
	r := referrer{Type: cl.ReferrerType, ID: idOf(cl.ReferrerID), Code: cl.Code, ClickID: id, StandeeID: idOf(cl.StandeeID)}
	if r.Type == partyStore {
		biz, err := s.q(ctx).StoreBusiness(ctx, cl.ReferrerID)
		if err != nil {
			return referrer{}, time.Time{}, false, err
		}
		r.BusinessID = idOf(biz)
	}
	return r, cl.CreatedAt, true, nil
}
