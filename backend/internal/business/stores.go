package business

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// Slug and short-code shapes (03 ck_stores_slug, ck_stores_short_code).
const (
	slugBaseMax   = 48 // leaves room for a "-xxxx" suffix under the 64 limit
	slugSuffixLen = 4
	shortCodeLen  = 6
	slugAlphabet  = "abcdefghijkmnpqrstuvwxyz23456789"
	slugFallback  = "store"
)

// Slugify makes a store slug from free text: ASCII lower-case letters and
// digits joined by single hyphens, at most slugBaseMax characters. Text with
// no ASCII letter or digit (a name in Devanagari) gives "store".
func Slugify(parts ...string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(strings.Join(parts, " ")) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(r)
		default:
			hyphen = true
		}
		if b.Len() >= slugBaseMax {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > slugBaseMax {
		s = strings.Trim(s[:slugBaseMax], "-")
	}
	if s == "" {
		return slugFallback
	}
	return s
}

// --- hours conversions ---------------------------------------------------------------

func rangesFromAPI(day *businessapi.HoursDay) []Range {
	if day == nil {
		return []Range{}
	}
	out := make([]Range, 0, len(*day))
	for _, r := range *day {
		out = append(out, Range{Open: r.Open, Close: r.Close})
	}
	return out
}

func rangesToAPI(rs []Range) businessapi.HoursDay {
	out := make(businessapi.HoursDay, 0, len(rs))
	for _, r := range rs {
		out = append(out, businessapi.HoursRange{Open: r.Open, Close: r.Close})
	}
	return out
}

// HoursFromAPI converts the contract's hours into the stored form.
func HoursFromAPI(h businessapi.StoreHours) Hours {
	w := h.Weekly
	out := Hours{Weekly: map[string][]Range{
		"mon": rangesFromAPI(w.Mon), "tue": rangesFromAPI(w.Tue), "wed": rangesFromAPI(w.Wed),
		"thu": rangesFromAPI(w.Thu), "fri": rangesFromAPI(w.Fri), "sat": rangesFromAPI(w.Sat),
		"sun": rangesFromAPI(w.Sun),
	}, Holidays: make([]Holiday, 0, len(h.Holidays))}
	for _, hd := range h.Holidays {
		out.Holidays = append(out.Holidays, Holiday{Date: hd.Date, Ranges: rangesFromAPI(&hd.Ranges)})
	}
	return out
}

// HoursToAPI converts stored hours into the contract's, every weekday present.
func HoursToAPI(h Hours) businessapi.StoreHours {
	day := func(k string) *businessapi.HoursDay { d := rangesToAPI(h.Weekly[k]); return &d }
	out := businessapi.StoreHours{
		Weekly: businessapi.StoreWeek{
			Mon: day("mon"), Tue: day("tue"), Wed: day("wed"), Thu: day("thu"),
			Fri: day("fri"), Sat: day("sat"), Sun: day("sun"),
		},
		Holidays: make([]businessapi.StoreHoliday, 0, len(h.Holidays)),
	}
	for _, hd := range h.Holidays {
		out.Holidays = append(out.Holidays, businessapi.StoreHoliday{Date: hd.Date, Ranges: rangesToAPI(hd.Ranges)})
	}
	return out
}

// storedHours validates and serialises hours from a request (path prefix for
// the field errors).
func storedHours(v *invalid, prefix string, h businessapi.StoreHours) []byte {
	hours := HoursFromAPI(h)
	if errs := hours.Validate(prefix); len(errs) > 0 {
		v.addAll(errs)
		return nil
	}
	raw, err := hours.Marshal()
	if err != nil {
		v.add(prefix, "must be valid hours")
		return nil
	}
	return raw
}

// --- view ----------------------------------------------------------------------

func (s *Service) toStore(st businessdb.GetStoreRow, now time.Time) (businessapi.Store, error) {
	h, err := ParseHours(st.Hours)
	if err != nil {
		return businessapi.Store{}, err
	}
	out := businessapi.Store{
		AddressLine1: st.AddressLine1, AddressLine2: st.AddressLine2,
		BusinessId: idString(st.BusinessID), City: st.City, CreatedAt: st.CreatedAt.UTC(),
		Hours: HoursToAPI(h), Id: idString(st.ID), Landmark: st.Landmark, Locality: st.Locality,
		Name: st.Name, OpenNow: h.OpenAt(now, st.ClosedUntil), Pincode: st.Pincode,
		ShortCode: st.ShortCode, Slug: st.Slug, Status: businessapi.StoreStatus(st.Status),
	}
	if st.HasLocation {
		out.Location = &businessapi.StoreLocation{Lat: st.Lat, Lng: st.Lng}
	}
	if st.PhoneEnc != nil {
		phone, err := s.storePhone(st)
		if err != nil {
			return businessapi.Store{}, err
		}
		out.Phone = &phone
	}
	return out, nil
}

// storePhone opens a store's phone (sealed for that store).
func (s *Service) storePhone(st businessdb.GetStoreRow) (string, error) {
	sid, _ := ids.FromUUID(st.ID)
	plain, err := s.seal.Open(SealField(FieldPhone, sid), st.PhoneEnc)
	if err != nil {
		return "", fmt.Errorf("business: open store phone: %w", err)
	}
	return string(plain), nil
}

func (s *Service) loadStore(ctx context.Context, bid, sid ids.ID) (businessdb.GetStoreRow, error) {
	st, err := s.q(ctx).GetStore(ctx, ids.UUID(sid))
	if db.IsNoRows(err) || (err == nil && st.BusinessID != ids.UUID(bid)) {
		return businessdb.GetStoreRow{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return businessdb.GetStoreRow{}, fmt.Errorf("business: store: %w", err)
	}
	return st, nil
}

func storePath(bidStr, sidStr string) (ids.ID, ids.ID, error) {
	bid, err := pathID(bidStr)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	sid, err := pathID(sidStr)
	if err != nil {
		return ids.Zero, ids.Zero, err
	}
	return bid, sid, nil
}

// --- list and get ------------------------------------------------------------------

func (s *Service) listStores(ctx context.Context, bidStr string) (businessapi.StoreList, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StoreList{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.StoreList{}, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.StoreList{}, err
	}
	m, err := s.membership(ctx, p.UserID, bid)
	if err != nil {
		return businessapi.StoreList{}, err
	}
	rows, err := s.q(ctx).ListStores(ctx, ids.UUID(bid))
	if err != nil {
		return businessapi.StoreList{}, fmt.Errorf("business: stores: %w", err)
	}
	now := s.clock.Now()
	out := businessapi.StoreList{Items: make([]businessapi.Store, 0, len(rows))}
	for _, r := range rows {
		if m.StoreIDs != nil {
			id, _ := ids.FromUUID(r.ID)
			if !slices.Contains(m.StoreIDs, id) {
				continue
			}
		}
		st, err := s.toStore(businessdb.GetStoreRow(r), now)
		if err != nil {
			return businessapi.StoreList{}, err
		}
		out.Items = append(out.Items, st)
	}
	return out, nil
}

func (s *Service) getStore(ctx context.Context, bidStr, sidStr string) (businessapi.Store, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Store{}, 0, err
	}
	bid, sid, err := storePath(bidStr, sidStr)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	st, err := s.loadStore(ctx, bid, sid)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	out, err := s.toStore(st, s.clock.Now())
	return out, st.Version, err
}

// --- create ----------------------------------------------------------------------

// sealPhone normalises and seals a store phone for store sid (nil: no
// phone), and returns the normalised number too. The contract has already
// checked the pattern; this is the strict mobile check.
func (s *Service) sealPhone(v *invalid, path string, sid ids.ID, phone *string) ([]byte, string) {
	if phone == nil {
		return nil, ""
	}
	e164, ok := pii.NormalizePhone(*phone)
	if !ok {
		v.add(path, "must be an Indian mobile number")
		return nil, ""
	}
	enc, err := s.seal.Seal(SealField(FieldPhone, sid), []byte(e164))
	if err != nil {
		v.add(path, "could not be stored")
		return nil, ""
	}
	return enc, e164
}

func (s *Service) createStore(ctx context.Context, bidStr string, body *businessapi.StoreCreate) (businessapi.Store, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Store{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	if body == nil {
		return businessapi.Store{}, 0, problem.New(genproblems.BadRequest, "")
	}
	m, err := s.membership(ctx, p.UserID, bid)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	if m.StoreIDs != nil {
		// TODO(lead): a manager limited to some stores may not add one (the
		// new store would sit outside their own scope); 02 §3.3 does not say.
		return businessapi.Store{}, 0, problem.New(genproblems.Forbidden, "")
	}
	st, err := s.insertStore(ctx, audit.ActorStaff, p.UserID, bid, body, nil)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	out, err := s.toStore(st, s.clock.Now())
	return out, st.Version, err
}

// insertStore adds a draft store to the business and audits it as the actor
// (kind, uid). check, when set, runs first in the store's transaction (a
// field agent's edit window).
func (s *Service) insertStore(ctx context.Context, kind string, uid, bid ids.ID, body *businessapi.StoreCreate, check func(context.Context) error) (businessdb.GetStoreRow, error) {
	// The id comes first: the phone is sealed for it, and every retry below
	// reuses it (a failed attempt rolled back, so the id is still free).
	sid := ids.New(s.clock.Now())
	var v invalid
	v.text("body.name", &body.Name)
	v.text("body.addressLine1", body.AddressLine1)
	v.text("body.addressLine2", body.AddressLine2)
	v.text("body.landmark", body.Landmark)
	v.text("body.locality", body.Locality)
	v.text("body.city", body.City)
	phoneEnc, _ := s.sealPhone(&v, "body.phone", sid, body.Phone)
	// No hours yet is a store never open; the submit asks for them.
	hours := storedHours(&v, "body.hours", businessapi.StoreHours{})
	if body.Hours != nil {
		hours = storedHours(&v, "body.hours", *body.Hours)
	}
	if err := v.err(); err != nil {
		return businessdb.GetStoreRow{}, err
	}
	var lat, lng *float64
	if body.Location != nil {
		lat, lng = &body.Location.Lat, &body.Location.Lng
	}
	locality := ""
	if body.Locality != nil {
		locality = *body.Locality
	}
	base := Slugify(body.Name, locality)

	for attempt := 0; ; attempt++ {
		slug, code, err := s.storeCodes(ctx, base, attempt)
		if err != nil {
			return businessdb.GetStoreRow{}, err
		}
		err = s.db.InTx(ctx, func(ctx context.Context) error {
			if err := s.businessOpen(ctx, bid); err != nil {
				return err
			}
			if check != nil {
				if err := check(ctx); err != nil {
					return err
				}
			}
			now := s.clock.Now()
			if err := s.q(ctx).InsertStore(ctx, businessdb.InsertStoreParams{
				ID: ids.UUID(sid), BusinessID: ids.UUID(bid), Slug: slug, ShortCode: code, Name: body.Name,
				AddressLine1: body.AddressLine1, AddressLine2: body.AddressLine2, Landmark: body.Landmark,
				Locality: body.Locality, City: body.City, Pincode: body.Pincode, Lat: lat, Lng: lng,
				PhoneEnc: phoneEnc, Hours: json.RawMessage(hours), Now: now,
			}); err != nil {
				return err
			}
			return s.writeAuditAs(ctx, kind, uid, "store.create", "store", sid.String(), nil,
				map[string]any{"name": body.Name, "slug": slug, "shortCode": code, "businessId": bid.String()})
		})
		retry := db.IsUniqueViolation(err, "ux_stores_slug") || db.IsUniqueViolation(err, "ux_stores_short_code")
		if retry && attempt < codeAttempts-1 {
			continue
		}
		if err != nil {
			return businessdb.GetStoreRow{}, fmt.Errorf("business: create store: %w", err)
		}
		break
	}
	return s.loadStore(ctx, bid, sid)
}

// storeCodes picks a slug (base, or base plus a random suffix when base is
// taken or a try collided) and a random short code.
func (s *Service) storeCodes(ctx context.Context, base string, attempt int) (string, string, error) {
	slug := base
	taken := attempt > 0
	if !taken {
		t, err := s.q(ctx).SlugTaken(ctx, base)
		if err != nil {
			return "", "", fmt.Errorf("business: slug: %w", err)
		}
		taken = t
	}
	if taken {
		suffix, err := randomString(slugAlphabet, slugSuffixLen)
		if err != nil {
			return "", "", err
		}
		slug = base + "-" + suffix
	}
	code, err := randomString(codeAlphabet, shortCodeLen)
	if err != nil {
		return "", "", err
	}
	return slug, code, nil
}

// --- update ----------------------------------------------------------------------

func (s *Service) updateStore(ctx context.Context, bidStr, sidStr string, ifMatchHdr *string, body *businessapi.StoreUpdate) (businessapi.Store, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Store{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	bid, sid, err := storePath(bidStr, sidStr)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	version, err := ifMatch(ifMatchHdr)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	if body == nil {
		return businessapi.Store{}, 0, problem.New(genproblems.BadRequest, "")
	}
	params := businessdb.UpdateStoreProfileParams{ID: ids.UUID(sid), BusinessID: ids.UUID(bid), Version: version}
	var v invalid
	var changed []string
	text := func(key string, val *string, set *bool, dst **string) {
		if !sent(ctx, key) {
			return
		}
		v.text("body."+key, val)
		*set, *dst = true, val
		changed = append(changed, key)
	}
	if sent(ctx, "name") && body.Name != nil {
		v.text("body.name", body.Name)
		params.SetName, params.Name = true, *body.Name
		changed = append(changed, "name")
	}
	text("addressLine1", body.AddressLine1, &params.SetAddressLine1, &params.AddressLine1)
	text("addressLine2", body.AddressLine2, &params.SetAddressLine2, &params.AddressLine2)
	text("landmark", body.Landmark, &params.SetLandmark, &params.Landmark)
	text("locality", body.Locality, &params.SetLocality, &params.Locality)
	text("city", body.City, &params.SetCity, &params.City)
	text("pincode", body.Pincode, &params.SetPincode, &params.Pincode)
	if sent(ctx, "location") {
		params.SetLocation = true
		if body.Location != nil {
			params.Lat, params.Lng = &body.Location.Lat, &body.Location.Lng
		}
		changed = append(changed, "location")
	}
	newPhone := ""
	if sent(ctx, "phone") {
		params.SetPhone = true
		params.PhoneEnc, newPhone = s.sealPhone(&v, "body.phone", sid, body.Phone)
		changed = append(changed, "phone")
	}
	if err := v.err(); err != nil {
		return businessapi.Store{}, 0, err
	}

	err = s.db.InTx(ctx, func(ctx context.Context) error {
		if err := s.businessOpen(ctx, bid); err != nil {
			return err
		}
		cur, err := s.loadStore(ctx, bid, sid)
		if err != nil {
			return err
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		params.NeedsReview, err = s.profileChanged(cur, params, newPhone)
		if err != nil {
			return err
		}
		n, err := s.q(ctx).UpdateStoreProfile(ctx, params)
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		after := cur.Status
		if params.NeedsReview && (cur.Status == storeLive || cur.Status == storePaused) {
			after = storePendingReview
		}
		// Field names only: an address or a phone never goes in the audit log.
		return s.writeAudit(ctx, p.UserID, "store.update", "store", sid.String(),
			map[string]any{"status": cur.Status}, map[string]any{"status": after, "fields": changed})
	})
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	return s.getStore(ctx, bidStr, sidStr)
}

// profileChanged reports whether an update really changes what review
// approved (D-40): the name, the address, the location or the phone. A field
// sent with its current value is no change, so saving an unchanged form does
// not take a live store off the map. newPhone is the normalised number sent.
func (s *Service) profileChanged(cur businessdb.GetStoreRow, p businessdb.UpdateStoreProfileParams, newPhone string) (bool, error) {
	text := func(set bool, next, now *string) bool {
		return set && !equalText(next, now)
	}
	if (p.SetName && p.Name != cur.Name) ||
		text(p.SetAddressLine1, p.AddressLine1, cur.AddressLine1) ||
		text(p.SetAddressLine2, p.AddressLine2, cur.AddressLine2) ||
		text(p.SetLandmark, p.Landmark, cur.Landmark) ||
		text(p.SetLocality, p.Locality, cur.Locality) ||
		text(p.SetCity, p.City, cur.City) ||
		text(p.SetPincode, p.Pincode, cur.Pincode) {
		return true, nil
	}
	if p.SetLocation {
		if (p.Lat != nil) != cur.HasLocation {
			return true, nil
		}
		if p.Lat != nil && (*p.Lat != cur.Lat || *p.Lng != cur.Lng) {
			return true, nil
		}
	}
	if p.SetPhone {
		if (p.PhoneEnc != nil) != (cur.PhoneEnc != nil) {
			return true, nil
		}
		if cur.PhoneEnc != nil {
			old, err := s.storePhone(cur)
			if err != nil {
				return false, err
			}
			if old != newPhone {
				return true, nil
			}
		}
	}
	return false, nil
}

func equalText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// --- submit ----------------------------------------------------------------------

// submitStore sends a draft store for review (D-40).
//
// TODO(lead): a store may be submitted before its business is verified; the
// admin review (W1-10) decides both. 01 §3 does not order them.
func (s *Service) submitStore(ctx context.Context, bidStr, sidStr string) (businessapi.Store, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Store{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	bid, sid, err := storePath(bidStr, sidStr)
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := s.loadStore(ctx, bid, sid)
		if err != nil {
			return err
		}
		if cur.Status != storeDraft {
			return problem.New(genproblems.InvalidTransition, "")
		}
		rows, err := q.CountSubmitStores(ctx, ids.UUID(bid))
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.ID != cur.ID {
				continue
			}
			if gaps := storeGaps("", r); len(gaps) > 0 {
				return incomplete(gaps)
			}
		}
		n, err := q.SubmitStore(ctx, businessdb.SubmitStoreParams{ID: cur.ID, BusinessID: cur.BusinessID})
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.New(genproblems.InvalidTransition, "")
		}
		return s.writeAudit(ctx, p.UserID, "store.submit", "store", sid.String(),
			map[string]any{"status": cur.Status}, map[string]any{"status": "pending_review"})
	})
	if err != nil {
		return businessapi.Store{}, 0, err
	}
	return s.getStore(ctx, bidStr, sidStr)
}

// --- settings --------------------------------------------------------------------

// settingsView is the audited and returned form of a store's settings.
type settingsView struct {
	Hours                  json.RawMessage `json:"hours"`
	ClosedUntil            *time.Time      `json:"closedUntil"`
	AcceptsPickup          bool            `json:"acceptsPickup"`
	AcceptsDelivery        bool            `json:"acceptsDelivery"`
	AsksMuted              bool            `json:"asksMuted"`
	DeliveryRadiusM        int32           `json:"deliveryRadiusM"`
	DeliveryFeePaise       int64           `json:"deliveryFeePaise"`
	DeliveryFreeAbovePaise *int64          `json:"deliveryFreeAbovePaise"`
	DeliveryMinOrderPaise  int64           `json:"deliveryMinOrderPaise"`
	DeliveryHours          json.RawMessage `json:"deliveryHours"`
	CashbackBp             int32           `json:"cashbackBp"`
	AutoAccept             bool            `json:"autoAccept"`
}

func settingsOf(st businessdb.GetStoreRow) settingsView {
	return settingsView{
		Hours: st.Hours, ClosedUntil: st.ClosedUntil, AcceptsPickup: st.AcceptsPickup,
		AcceptsDelivery: st.AcceptsDelivery, AsksMuted: st.AsksMuted, DeliveryRadiusM: st.DeliveryRadiusM,
		DeliveryFeePaise: st.DeliveryFeePaise, DeliveryFreeAbovePaise: st.DeliveryFreeAbovePaise,
		DeliveryMinOrderPaise: st.DeliveryMinOrderPaise, DeliveryHours: st.DeliveryHours,
		CashbackBp: st.CashbackBp, AutoAccept: st.AutoAccept,
	}
}

func toSettings(st businessdb.GetStoreRow, now time.Time) (businessapi.StoreSettings, error) {
	h, err := ParseHours(st.Hours)
	if err != nil {
		return businessapi.StoreSettings{}, err
	}
	out := businessapi.StoreSettings{
		AcceptsDelivery: st.AcceptsDelivery, AcceptsPickup: st.AcceptsPickup, AsksMuted: st.AsksMuted,
		AutoAccept: st.AutoAccept, CashbackBp: int(st.CashbackBp), ClosedUntil: st.ClosedUntil,
		DeliveryFeePaise: int(st.DeliveryFeePaise), DeliveryMinOrderPaise: int(st.DeliveryMinOrderPaise),
		DeliveryRadiusM: int(st.DeliveryRadiusM), Hours: HoursToAPI(h),
		InStoreOnly: !st.AcceptsPickup && !st.AcceptsDelivery,
		OpenNow:     h.OpenAt(now, st.ClosedUntil),
	}
	if st.DeliveryFreeAbovePaise != nil {
		v := int(*st.DeliveryFreeAbovePaise)
		out.DeliveryFreeAbovePaise = &v
	}
	if st.DeliveryHours != nil {
		dh, err := ParseHours(st.DeliveryHours)
		if err != nil {
			return businessapi.StoreSettings{}, err
		}
		api := HoursToAPI(dh)
		out.DeliveryHours = &api
	}
	return out, nil
}

func (s *Service) getSettings(ctx context.Context, bidStr, sidStr string) (businessapi.StoreSettings, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	bid, sid, err := storePath(bidStr, sidStr)
	if err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	st, err := s.loadStore(ctx, bid, sid)
	if err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	out, err := toSettings(st, s.clock.Now())
	return out, st.Version, err
}

// updateSettings changes a store's hours, manual close, pickup and delivery
// options, cashback, auto-accept and asks mute. Fields left out keep their
// value; a change to asksMuted also needs asks.manage (D-101). The values
// before and after are audited (none of them is personal data).
func (s *Service) updateSettings(ctx context.Context, bidStr, sidStr string, ifMatchHdr *string, body *businessapi.StoreSettingsUpdate) (businessapi.StoreSettings, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	bid, sid, err := storePath(bidStr, sidStr)
	if err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	version, err := ifMatch(ifMatchHdr)
	if err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	if body == nil {
		return businessapi.StoreSettings{}, 0, problem.New(genproblems.BadRequest, "")
	}
	if sent(ctx, "asksMuted") {
		if err := s.also(ctx, policy.AsksManage, authz.Scope{BusinessID: bid, StoreID: sid}); err != nil {
			return businessapi.StoreSettings{}, 0, err
		}
	}

	var v invalid
	var hours, deliveryHours []byte
	if sent(ctx, "hours") && body.Hours != nil {
		hours = storedHours(&v, "body.hours", *body.Hours)
	}
	if sent(ctx, "deliveryHours") && body.DeliveryHours != nil {
		deliveryHours = storedHours(&v, "body.deliveryHours", *body.DeliveryHours)
	}
	if err := v.err(); err != nil {
		return businessapi.StoreSettings{}, 0, err
	}

	err = s.db.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.loadStore(ctx, bid, sid)
		if err != nil {
			return err
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		before := settingsOf(cur)
		next := before
		if hours != nil {
			next.Hours = hours
		}
		if sent(ctx, "closedUntil") {
			next.ClosedUntil = nil
			if body.ClosedUntil != nil {
				t := body.ClosedUntil.UTC()
				next.ClosedUntil = &t
			}
		}
		if body.AcceptsPickup != nil {
			next.AcceptsPickup = *body.AcceptsPickup
		}
		if body.AcceptsDelivery != nil {
			next.AcceptsDelivery = *body.AcceptsDelivery
		}
		if body.AsksMuted != nil {
			next.AsksMuted = *body.AsksMuted
		}
		if body.DeliveryRadiusM != nil {
			next.DeliveryRadiusM = int32(*body.DeliveryRadiusM) // #nosec G115 -- the validator bounds it to 100..20000 (ck_stores_delivery_radius)
		}
		if body.DeliveryFeePaise != nil {
			next.DeliveryFeePaise = int64(*body.DeliveryFeePaise)
		}
		if sent(ctx, "deliveryFreeAbovePaise") {
			next.DeliveryFreeAbovePaise = nil
			if body.DeliveryFreeAbovePaise != nil {
				n := int64(*body.DeliveryFreeAbovePaise)
				next.DeliveryFreeAbovePaise = &n
			}
		}
		if body.DeliveryMinOrderPaise != nil {
			next.DeliveryMinOrderPaise = int64(*body.DeliveryMinOrderPaise)
		}
		if sent(ctx, "deliveryHours") {
			next.DeliveryHours = nil
			if deliveryHours != nil {
				next.DeliveryHours = deliveryHours
			}
		}
		if body.CashbackBp != nil {
			next.CashbackBp = int32(*body.CashbackBp) // #nosec G115 -- the validator bounds it to 0..2000 (ck_stores_cashback)
		}
		if body.AutoAccept != nil {
			next.AutoAccept = *body.AutoAccept
		}
		n, err := s.q(ctx).UpdateStoreSettings(ctx, businessdb.UpdateStoreSettingsParams{
			Hours: next.Hours, ClosedUntil: next.ClosedUntil, AcceptsPickup: next.AcceptsPickup,
			AcceptsDelivery: next.AcceptsDelivery, AsksMuted: next.AsksMuted, DeliveryRadiusM: next.DeliveryRadiusM,
			DeliveryFeePaise: next.DeliveryFeePaise, DeliveryFreeAbovePaise: next.DeliveryFreeAbovePaise,
			DeliveryMinOrderPaise: next.DeliveryMinOrderPaise, DeliveryHours: next.DeliveryHours,
			CashbackBp: next.CashbackBp, AutoAccept: next.AutoAccept,
			ID: cur.ID, BusinessID: cur.BusinessID, Version: version,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		return s.writeAudit(ctx, p.UserID, "store.settings_update", "store", sid.String(), before, next)
	})
	if err != nil {
		return businessapi.StoreSettings{}, 0, err
	}
	return s.getSettings(ctx, bidStr, sidStr)
}

// --- store ids from a request ----------------------------------------------------------

// storeIDs parses a request's store list: every id must be a store of the
// business, and no id may repeat.
func (s *Service) storeIDs(ctx context.Context, v *invalid, path string, bid ids.ID, in []string) []pgtype.UUID {
	all, err := s.q(ctx).StoresOfBusiness(ctx, ids.UUID(bid))
	if err != nil {
		v.add(path, "could not be checked")
		return nil
	}
	out := make([]pgtype.UUID, 0, len(in))
	for i, sidStr := range in {
		p := fmt.Sprintf("%s[%d]", path, i)
		id, err := ids.Parse(sidStr)
		if err != nil {
			v.add(p, "must be an id")
			continue
		}
		u := ids.UUID(id)
		switch {
		case !slices.Contains(all, u):
			v.add(p, "must be a store of this business")
		case slices.Contains(out, u):
			v.add(p, "must not repeat")
		default:
			out = append(out, u)
		}
	}
	return out
}
