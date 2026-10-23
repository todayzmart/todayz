package seed

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// The business seed is the shop world of the manual test book (docs/qa/manual
// README §6): the Bengaluru stores around the QA point, a Hyderabad store and
// Marina Mart with twelve neighbours in Chennai. Every name is made up. The
// data is the same in every profile; ids, slugs, short codes and referral
// codes are fixed, and a second run changes nothing (ON CONFLICT DO NOTHING).
//
// Without PII_ENC_KEY and PII_HASH_KEY it still seeds the businesses and
// stores, but no staff (identity seeds no users then), no store phones and no
// KYC.
//
// TODO(lead): the book's people (O20 owner, M21, K22, R23, M24, X25, O26–O29)
// are not the identity seed's (+91 90000 00011–13, which the book lists as
// customers C11–C13). This seed links the identity seed's three staff users
// to Lakshmi Traders as owner, manager and cashier (Sri Lakshmi only); the
// other businesses have no staff until identity seeds their owners.

// QAPoint is the manual book's "location QA point" (Indiranagar, 560038).
var QAPoint = LatLng{Lat: 12.9719, Lng: 77.6412}

// LatLng is a WGS84 point.
type LatLng struct{ Lat, Lng float64 }

// offset is the point km away from p on a bearing (degrees from north), on
// a sphere; at these distances it is within metres of PostGIS's spheroid.
func (p LatLng) offset(bearing, km float64) LatLng {
	const earthKm = 6371.0088
	d := km / earthKm
	b := bearing * math.Pi / 180
	lat1, lng1 := p.Lat*math.Pi/180, p.Lng*math.Pi/180
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(d) + math.Cos(lat1)*math.Sin(d)*math.Cos(b))
	lng2 := lng1 + math.Atan2(math.Sin(b)*math.Sin(d)*math.Cos(lat1), math.Cos(d)-math.Sin(lat1)*math.Sin(lat2))
	return LatLng{Lat: lat2 * 180 / math.Pi, Lng: lng2 * 180 / math.Pi}
}

// Seed city centres for the stores outside Bengaluru.
var (
	hyderabadPoint = LatLng{Lat: 17.4156, Lng: 78.4347} // Banjara Hills
	chennaiPoint   = LatLng{Lat: 13.0418, Lng: 80.2341} // T. Nagar
)

type seedBusiness struct {
	Key      string // stable handle (businessSeedID)
	ID       string // fixed ULID
	Brand    string
	Legal    string
	Status   string // businesses.status
	Referral string
	Verified time.Duration // verified this long before the seed clock (0: not verified)
	KYC      *seedKYC
}

// seedKYC is a business's payout details; every value is made up.
type seedKYC struct {
	PAN, UPI, Bank, IFSC, Legal string
	Status                      string
}

type seedStore struct {
	Key       string // the book's short name (SL, SL2, …): storeSeedID
	ID        string
	Business  string // seedBusiness.Key
	Name      string
	Slug      string
	ShortCode string
	Address   string
	Locality  string
	City      string
	Pincode   string
	At        LatLng
	Status    string        // stores.status
	LiveFor   time.Duration // live since this long before the seed clock
	Open      [2]string     // daily hours; empty: no hours yet
	Pickup    bool
	Delivery  bool
	RadiusM   int32 // 0: the column default
	FeeP      int64
	FreeP     int64 // 0: no free-delivery threshold
	MinP      int64
	Cashback  int32
	Auto      bool
	OwnerLine bool // the owner's reserved number is the store phone
}

const day = 24 * time.Hour

var seedBusinesses = []seedBusiness{
	{Key: "lakshmi", ID: "01KE0S1W6W8ZV7T1C3A6MB0001", Brand: "Lakshmi Traders", Legal: "Lakshmi Traders LLP", Status: "verified", Referral: "BMKT22", Verified: 120 * day,
		KYC: &seedKYC{PAN: "AAAFL0001K", UPI: "lakshmitraders.seed@oktz", Bank: "000000001001", IFSC: "TZSB0000001", Legal: "Lakshmi Traders LLP", Status: "verified"}},
	{Key: "annapurna", ID: "01KE0S1W6W8ZV7T1C3A6MB0002", Brand: "Annapurna Provisions", Status: "verified", Referral: "BANP23", Verified: 10 * day},
	{Key: "nandi", ID: "01KE0S1W6W8ZV7T1C3A6MB0003", Brand: "Nandi Hardware", Status: "verified", Referral: "BNDH24", Verified: 90 * day},
	{Key: "kaveri", ID: "01KE0S1W6W8ZV7T1C3A6MB0004", Brand: "Kaveri Fresh", Status: "verified", Referral: "BKVF25", Verified: 90 * day},
	{Key: "automart", ID: "01KE0S1W6W8ZV7T1C3A6MB0005", Brand: "Auto Mart", Status: "verified", Referral: "BATM26", Verified: 90 * day},
	{Key: "paused", ID: "01KE0S1W6W8ZV7T1C3A6MB0006", Brand: "Paused Store", Status: "verified", Referral: "BPSD27", Verified: 60 * day},
	{Key: "draft", ID: "01KE0S1W6W8ZV7T1C3A6MB0007", Brand: "Draft Store", Status: "draft", Referral: "BDRF28"},
	{Key: "suspended", ID: "01KE0S1W6W8ZV7T1C3A6MB0008", Brand: "Suspended Store", Status: "suspended", Referral: "BSSP29", Verified: 60 * day},
	{Key: "needsinfo", ID: "01KE0S1W6W8ZV7T1C3A6MB0009", Brand: "Needs Info Store", Status: "needs_info", Referral: "BNNF32"},
	{Key: "submitted", ID: "01KE0S1W6W8ZV7T1C3A6MB0010", Brand: "Submitted Store", Status: "submitted", Referral: "BSBM33"},
	{Key: "hyderabad", ID: "01KE0S1W6W8ZV7T1C3A6MB0011", Brand: "Charminar Kirana", Status: "verified", Referral: "BHYD34", Verified: 60 * day},
	{Key: "marina", ID: "01KE0S1W6W8ZV7T1C3A6MB0012", Brand: "Marina Traders", Status: "verified", Referral: "BMRN35", Verified: 60 * day},
}

// daily is the same hours every day.
func daily(open, close string) [2]string { return [2]string{open, close} }

var seedStores = []seedStore{
	{Key: "SL", ID: "01KE0S1W6W8ZV7T1C3A6MS0001", Business: "lakshmi", Name: "Sri Lakshmi", Slug: "sri-lakshmi-indiranagar", ShortCode: "SRKM22",
		Address: "14, 12th Main Road", Locality: "Indiranagar", City: "Bengaluru", Pincode: "560038", At: QAPoint.offset(0, 0.6),
		Status: "live", LiveFor: 120 * day, Open: daily("08:00", "22:00"), Pickup: true, Delivery: true,
		RadiusM: 3000, FeeP: 3000, FreeP: 50000, MinP: 15000, Cashback: 500, OwnerLine: true},
	{Key: "SL2", ID: "01KE0S1W6W8ZV7T1C3A6MS0002", Business: "lakshmi", Name: "Sri Lakshmi 2", Slug: "sri-lakshmi-2-hal-2nd-stage", ShortCode: "SRKN23",
		Address: "7, 80 Feet Road", Locality: "HAL 2nd Stage", City: "Bengaluru", Pincode: "560008", At: QAPoint.offset(180, 1.8),
		Status: "live", LiveFor: 60 * day, Open: daily("08:00", "22:00"), Pickup: true, OwnerLine: true},
	{Key: "AP", ID: "01KE0S1W6W8ZV7T1C3A6MS0003", Business: "annapurna", Name: "Annapurna Provisions", Slug: "annapurna-provisions-hal-2nd-stage", ShortCode: "ANPV24",
		Address: "22, Double Road", Locality: "HAL 2nd Stage", City: "Bengaluru", Pincode: "560008", At: QAPoint.offset(180, 1.2),
		Status: "live", LiveFor: 10 * day, Open: daily("09:00", "21:00"), Pickup: true},
	{Key: "NH", ID: "01KE0S1W6W8ZV7T1C3A6MS0004", Business: "nandi", Name: "Nandi Hardware", Slug: "nandi-hardware-indiranagar", ShortCode: "NDHW25",
		Address: "3, CMH Road", Locality: "Indiranagar", City: "Bengaluru", Pincode: "560038", At: QAPoint.offset(90, 0.9),
		Status: "live", LiveFor: 90 * day, Open: daily("10:00", "20:00")},
	{Key: "KF", ID: "01KE0S1W6W8ZV7T1C3A6MS0005", Business: "kaveri", Name: "Kaveri Fresh", Slug: "kaveri-fresh-domlur", ShortCode: "KVFR26",
		Address: "40, Old Airport Road", Locality: "Domlur", City: "Bengaluru", Pincode: "560071", At: QAPoint.offset(270, 3.5),
		Status: "live", LiveFor: 90 * day, Open: daily("07:00", "22:00"), Delivery: true, RadiusM: 5000, MinP: 10000, Cashback: 200},
	{Key: "AM", ID: "01KE0S1W6W8ZV7T1C3A6MS0006", Business: "automart", Name: "Auto Mart", Slug: "auto-mart-kalyan-nagar", ShortCode: "ATMT27",
		Address: "9, Outer Ring Road", Locality: "Kalyan Nagar", City: "Bengaluru", Pincode: "560043", At: QAPoint.offset(45, 4.2),
		Status: "live", LiveFor: 90 * day, Open: daily("08:00", "22:00"), Pickup: true, Auto: true},
	{Key: "PS", ID: "01KE0S1W6W8ZV7T1C3A6MS0007", Business: "paused", Name: "Paused Store", Slug: "paused-store-indiranagar", ShortCode: "PSDS28",
		Address: "5, 100 Feet Road", Locality: "Indiranagar", City: "Bengaluru", Pincode: "560038", At: QAPoint.offset(300, 1.0),
		Status: "paused", LiveFor: 60 * day, Pickup: true},
	{Key: "DS", ID: "01KE0S1W6W8ZV7T1C3A6MS0008", Business: "draft", Name: "Draft Store", Slug: "draft-store-indiranagar", ShortCode: "DRFS29",
		Locality: "Indiranagar", City: "Bengaluru", At: QAPoint.offset(330, 1.1), Status: "draft", Pickup: true},
	{Key: "PR", ID: "01KE0S1W6W8ZV7T1C3A6MS0009", Business: "automart", Name: "Auto Mart Indiranagar", Slug: "auto-mart-indiranagar", ShortCode: "ATMN32",
		Address: "61, 12th Main Road", Locality: "Indiranagar", City: "Bengaluru", Pincode: "560038", At: QAPoint.offset(30, 1.4),
		Status: "pending_review", Open: daily("08:00", "22:00"), Pickup: true},
	{Key: "SS", ID: "01KE0S1W6W8ZV7T1C3A6MS0010", Business: "suspended", Name: "Suspended Store", Slug: "suspended-store-indiranagar", ShortCode: "SSPS33",
		Address: "18, Defence Colony", Locality: "Indiranagar", City: "Bengaluru", Pincode: "560038", At: QAPoint.offset(120, 1.5),
		Status: "suspended", LiveFor: 60 * day, Open: daily("08:00", "22:00"), Pickup: true},
	{Key: "NI", ID: "01KE0S1W6W8ZV7T1C3A6MS0011", Business: "needsinfo", Name: "Needs Info Store", Slug: "needs-info-store-indiranagar", ShortCode: "NDNF34",
		Address: "2, Binnamangala", Locality: "Indiranagar", City: "Bengaluru", Pincode: "560038", At: QAPoint.offset(150, 1.6),
		Status: "pending_review", Open: daily("08:00", "22:00"), Pickup: true},
	{Key: "SB", ID: "01KE0S1W6W8ZV7T1C3A6MS0012", Business: "submitted", Name: "Submitted Store", Slug: "submitted-store-indiranagar", ShortCode: "SBMS35",
		Address: "30, Jeevan Bhima Nagar Main Road", Locality: "Jeevan Bhima Nagar", City: "Bengaluru", Pincode: "560075", At: QAPoint.offset(210, 1.7),
		Status: "pending_review", Open: daily("08:00", "22:00"), Pickup: true},
	{Key: "HY", ID: "01KE0S1W6W8ZV7T1C3A6MS0013", Business: "hyderabad", Name: "Charminar Kirana", Slug: "charminar-kirana-banjara-hills", ShortCode: "HYDK36",
		Address: "11, Road No. 12", Locality: "Banjara Hills", City: "Hyderabad", Pincode: "500034", At: hyderabadPoint,
		Status: "live", LiveFor: 60 * day, Open: daily("08:00", "22:00"), Pickup: true, Delivery: true, RadiusM: 3000},
	{Key: "MM", ID: "01KE0S1W6W8ZV7T1C3A6MS0014", Business: "marina", Name: "Marina Mart", Slug: "marina-mart-t-nagar", ShortCode: "MRNM37",
		Address: "8, Usman Road", Locality: "T. Nagar", City: "Chennai", Pincode: "600017", At: chennaiPoint,
		Status: "live", LiveFor: 60 * day, Open: daily("08:00", "22:00"), Pickup: true},
}

// chennaiNeighbours are "Chennai Store 01" to "12" (<seed:price-compare>):
// live, verified, each of its own business, all within 2 km of Marina Mart.
const chennaiNeighbours = 12

func init() {
	codes := "23456789ABCDEFGHJKMN" // short-code and referral suffixes, one per neighbour
	for i := 1; i <= chennaiNeighbours; i++ {
		n := fmt.Sprintf("%02d", i)
		key := "chennai" + n
		seedBusinesses = append(seedBusinesses, seedBusiness{
			Key: key, ID: "01KE0S1W6W8ZV7T1C3A6MB01" + n, Brand: "Chennai Store " + n, Status: "verified",
			Referral: "BCHN" + string(codes[i]) + "2", Verified: 60 * day,
		})
		seedStores = append(seedStores, seedStore{
			Key: "CH" + n, ID: "01KE0S1W6W8ZV7T1C3A6MS01" + n, Business: key, Name: "Chennai Store " + n,
			Slug: "chennai-store-" + n + "-t-nagar", ShortCode: "CHNS" + string(codes[i]) + "3",
			Address: n + ", Pondy Bazaar", Locality: "T. Nagar", City: "Chennai", Pincode: "600017",
			// Spread on a ring 0.4 to 1.5 km from Marina Mart.
			At:     chennaiPoint.offset(float64(i*30), 0.4+0.1*float64(i-1)),
			Status: "live", LiveFor: 60 * day, Open: daily("08:00", "22:00"), Pickup: true,
		})
	}
}

// seedStaff links the identity seed's staff users to Lakshmi Traders.
var seedStaff = []struct {
	ID, Business, User, Role string
	Stores                   []string // store keys; nil: every store
}{
	{ID: "01KE0S1W6W8ZV7T1C3A6MT0001", Business: "lakshmi", User: "staff.owner", Role: "owner"},
	{ID: "01KE0S1W6W8ZV7T1C3A6MT0002", Business: "lakshmi", User: "staff.manager", Role: "manager"},
	{ID: "01KE0S1W6W8ZV7T1C3A6MT0003", Business: "lakshmi", User: "staff.cashier", Role: "cashier", Stores: []string{"SL"}},
}

// businessSeedID returns a seeded business's fixed id by its key; it panics on
// an unknown key (a seed bug the seed test finds).
func businessSeedID(key string) ids.ID {
	for _, b := range seedBusinesses {
		if b.Key == key {
			return mustID(b.ID)
		}
	}
	panic("seed: no business " + key)
}

// storeSeedID returns a seeded store's fixed id by the book's short name
// (SL, SL2, AP, …, CH01–CH12), for the later domains' seeds.
func storeSeedID(key string) ids.ID {
	for _, s := range seedStores {
		if s.Key == key {
			return mustID(s.ID)
		}
	}
	panic("seed: no store " + key)
}

func (s seedStore) hours() ([]byte, error) {
	h := business.Hours{Weekly: map[string][]business.Range{}, Holidays: []business.Holiday{}}
	if s.Open[0] != "" {
		for _, d := range business.Weekdays {
			h.Weekly[d] = []business.Range{{Open: s.Open[0], Close: s.Open[1]}}
		}
	}
	if errs := h.Validate("hours"); len(errs) > 0 {
		return nil, fmt.Errorf("store %s: hours: %s %s", s.Key, errs[0].Path, errs[0].Reason)
	}
	return h.Marshal()
}

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// seedbusiness writes the businesses, stores, staff and KYC above.
func seedbusiness(ctx context.Context, d Deps, _ Profile) error {
	var seal *pii.Sealer
	if d.Keys.PIIEncKey.IsSet() && d.Keys.PIIHashKey.IsSet() {
		var err error
		if seal, err = pii.NewSealer(d.Keys.PIIEncKey.Bytes()); err != nil {
			return fmt.Errorf("PII_ENC_KEY: %w", err)
		}
	} else {
		d.Log.WarnContext(ctx, "seed business: PII_ENC_KEY or PII_HASH_KEY unset; no staff, store phones or KYC seeded (run make setup)")
	}
	now := d.Clock.Now().UTC()
	q := d.DB.Q(ctx)
	for _, b := range seedBusinesses {
		var verifiedAt, submittedAt *time.Time
		if b.Verified > 0 {
			t := now.Add(-b.Verified)
			verifiedAt = &t
			s := t.Add(-2 * day)
			submittedAt = &s
		} else if b.Status != "draft" {
			s := now.Add(-2 * day)
			submittedAt = &s
		}
		var fields []string
		var note *string
		if b.Status == "needs_info" {
			fields, note = []string{"storefrontPhoto"}, opt("A clearer storefront photo, please.")
		}
		if _, err := q.Exec(ctx, `INSERT INTO businesses (id, brand_name, legal_name, status, referral_code,
				needs_info_fields, needs_info_note, submitted_at, verified_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10) ON CONFLICT DO NOTHING`,
			ids.UUID(mustID(b.ID)), b.Brand, opt(b.Legal), b.Status, b.Referral, fields, note,
			submittedAt, verifiedAt, now.Add(-150*day)); err != nil {
			return fmt.Errorf("business %s: %w", b.Key, err)
		}
		if b.KYC != nil && seal != nil {
			if err := seedKYCRow(ctx, d, seal, b, verifiedAt); err != nil {
				return err
			}
		}
	}
	for _, s := range seedStores {
		raw, err := s.hours()
		if err != nil {
			return err
		}
		var liveAt *time.Time
		if s.LiveFor > 0 {
			t := now.Add(-s.LiveFor)
			liveAt = &t
		}
		var phone []byte
		if s.OwnerLine && seal != nil {
			e164 := identityPhone("staff.owner")
			if phone, err = seal.Seal(business.FieldPhone, []byte(e164)); err != nil {
				return fmt.Errorf("store %s: %w", s.Key, err)
			}
		}
		var free *int64
		if s.FreeP > 0 {
			free = &s.FreeP
		}
		tag, err := q.Exec(ctx, `INSERT INTO stores (id, business_id, slug, short_code, name, address_line1,
				locality, city, pincode, location, phone_enc, hours, status, live_at, accepts_pickup, accepts_delivery,
				delivery_fee_paise, delivery_free_above_paise, delivery_min_order_paise,
				cashback_bp, auto_accept, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
				ST_SetSRID(ST_MakePoint($10, $11), 4326)::geography, $12, $13, $14, $15, $16, $17,
				$18, $19, $20, $21, $22, $23, $23) ON CONFLICT DO NOTHING`,
			ids.UUID(mustID(s.ID)), ids.UUID(businessSeedID(s.Business)), s.Slug, s.ShortCode, s.Name, opt(s.Address),
			opt(s.Locality), opt(s.City), opt(s.Pincode), s.At.Lng, s.At.Lat, phone, raw, s.Status, liveAt, s.Pickup, s.Delivery,
			s.FeeP, free, s.MinP, s.Cashback, s.Auto, now.Add(-150*day))
		if err != nil {
			return fmt.Errorf("store %s: %w", s.Key, err)
		}
		// A store without a radius keeps the column default; only a row this
		// run inserted gets one, so a second run never undoes an edit.
		if s.RadiusM > 0 && tag.RowsAffected() == 1 {
			if _, err := q.Exec(ctx, `UPDATE stores SET delivery_radius_m = $2 WHERE id = $1`,
				ids.UUID(mustID(s.ID)), s.RadiusM); err != nil {
				return fmt.Errorf("store %s: radius: %w", s.Key, err)
			}
		}
	}
	if seal == nil {
		return nil
	}
	for _, st := range seedStaff {
		var stores []any
		for _, k := range st.Stores {
			stores = append(stores, ids.UUID(storeSeedID(k)))
		}
		var storeIDs any
		if stores != nil {
			storeIDs = stores
		}
		if _, err := q.Exec(ctx, `INSERT INTO staff (id, business_id, user_id, role, store_ids, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5::uuid[], 'active', $6, $6) ON CONFLICT DO NOTHING`,
			ids.UUID(mustID(st.ID)), ids.UUID(businessSeedID(st.Business)), ids.UUID(identityUserID(st.User)),
			st.Role, storeIDs, now.Add(-150*day)); err != nil {
			return fmt.Errorf("staff %s: %w", st.ID, err)
		}
	}
	d.Log.DebugContext(ctx, "seed business", slog.Int("businesses", len(seedBusinesses)), slog.Int("stores", len(seedStores)))
	return nil
}

// seedKYCRow seals a business's made-up payout details the way the service does.
func seedKYCRow(ctx context.Context, d Deps, seal *pii.Sealer, b seedBusiness, verifiedAt *time.Time) error {
	k := b.KYC
	if !business.ValidPAN(k.PAN) {
		return fmt.Errorf("business %s: the seeded PAN is not a PAN", b.Key)
	}
	sealed := map[string][]byte{}
	for field, v := range map[string]string{business.FieldPAN: k.PAN, business.FieldUPI: k.UPI, business.FieldBankAccount: k.Bank} {
		enc, err := seal.Seal(field, []byte(v))
		if err != nil {
			return fmt.Errorf("business %s: kyc: %w", b.Key, err)
		}
		sealed[field] = enc
	}
	upiName := k.UPI
	for i := range upiName {
		if upiName[i] == '@' {
			upiName = upiName[:i]
			break
		}
	}
	if _, err := d.DB.Q(ctx).Exec(ctx, `INSERT INTO business_kyc (business_id, pan_enc, pan_last4, upi_id_enc, upi_id_last4,
			bank_account_enc, bank_account_last4, bank_ifsc, legal_name, status, verified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) ON CONFLICT DO NOTHING`,
		ids.UUID(mustID(b.ID)), sealed[business.FieldPAN], k.PAN[len(k.PAN)-4:], sealed[business.FieldUPI], upiName[len(upiName)-4:],
		sealed[business.FieldBankAccount], k.Bank[len(k.Bank)-4:], k.IFSC, k.Legal, k.Status, verifiedAt); err != nil {
		return fmt.Errorf("business %s: kyc: %w", b.Key, err)
	}
	return nil
}

// identityPhone is a seeded user's phone by key.
func identityPhone(key string) string {
	for _, u := range identityUsers {
		if u.Key == key {
			return u.Phone
		}
	}
	panic("seed: no identity user " + key)
}
