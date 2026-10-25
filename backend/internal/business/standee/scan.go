package standee

import (
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	standeeapi "github.com/todayz/backend/gen/api/standee"
	"github.com/todayz/backend/gen/sqlc/standeedb"
)

// standeeRow is a standee with its counts, as ListStandees and GetStandee
// read it. The counts (D-223):
//   - scans: the referral_clicks rows with this standee_id, each an open of
//     its QR link that the click route matched to this store (SC-ST-03);
//   - signUps: the referrals made from those clicks, except rejected ones;
//   - qualified: those whose first qualifying purchase is done (qualified or
//     rewarded).
type standeeRow struct {
	ID                 pgtype.UUID
	StoreID            pgtype.UUID
	Campaign           string
	PrintedAt          *time.Time
	ReorderRequestedAt *time.Time
	CreatedAt          time.Time
	Version            int32
	Scans              int32
	LastScanAt         *time.Time
	SignUps            int32
	Qualified          int32
}

// row reads a generated row. max() over no clicks is NULL, which sqlc types
// as any: it is a time.Time or nil.
func row(r standeedb.ListStandeesRow) standeeRow {
	out := standeeRow{
		ID: r.ID, StoreID: r.StoreID, Campaign: r.Campaign, PrintedAt: r.PrintedAt,
		ReorderRequestedAt: r.ReorderRequestedAt, CreatedAt: r.CreatedAt, Version: r.Version,
		Scans: r.Scans, SignUps: r.SignUps, Qualified: r.Qualified,
	}
	if t, ok := r.LastScanAt.(time.Time); ok {
		t = t.UTC()
		out.LastScanAt = &t
	}
	return out
}

func (s *Service) toAPI(shortCode string, r standeeRow) standeeapi.Standee {
	id := uuidString(r.ID)
	return standeeapi.Standee{
		Id: id, StoreId: uuidString(r.StoreID), Campaign: r.Campaign, Link: s.link(shortCode, id),
		Counts:     standeeapi.StandeeCounts{Scans: int(r.Scans), SignUps: int(r.SignUps), Qualified: int(r.Qualified)},
		LastScanAt: r.LastScanAt, PrintedAt: utc(r.PrintedAt), ReorderRequestedAt: utc(r.ReorderRequestedAt),
		CreatedAt: r.CreatedAt.UTC(), Version: int(r.Version),
	}
}

// link is the URL the standee's QR carries (D-162): the store's short-code
// link with the standee id.
func (s *Service) link(shortCode, id string) string {
	return s.publicURL + "/v1/public/ref/" + url.PathEscape(shortCode) + "?s=" + url.QueryEscape(id)
}

func add(a, b standeeapi.StandeeCounts) standeeapi.StandeeCounts {
	return standeeapi.StandeeCounts{Scans: a.Scans + b.Scans, SignUps: a.SignUps + b.SignUps, Qualified: a.Qualified + b.Qualified}
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
