package insights

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	insightsapi "github.com/todayz/backend/gen/api/insights"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// phoneField is the pii field both phone_identities.phone_enc and the POS
// link_phone_enc are sealed under.
const phoneField = "phone"

// Customers is B-09: the store's customers of the regular window. all is
// newest visit first, top by spend, slipping the regulars behind the
// "Regulars slipping away" insight (by spend), empty below the linked
// customer minimum.
func (s *Service) Customers(ctx context.Context, storeID ids.ID, seg insightsapi.CustomerSegment, limit *int, cursor *string) (insightsapi.StoreCustomerList, error) {
	if err := s.ready(ctx); err != nil {
		return insightsapi.StoreCustomerList{}, err
	}
	lim, off, err := page(limit, cursor)
	if err != nil {
		return insightsapi.StoreCustomerList{}, err
	}
	st, err := facts.LoadSettings(ctx, s.settings)
	if err != nil {
		return insightsapi.StoreCustomerList{}, err
	}
	now := s.now()
	q := s.q(ctx)
	reg, err := facts.LoadRegulars(ctx, q, st, storeID, now)
	if err != nil {
		return insightsapi.StoreCustomerList{}, err
	}
	enough := reg.Enough(st)
	var rows []insightsdb.CustomerVisitsRow
	switch seg {
	case insightsapi.Slipping:
		if enough {
			rows = reg.Slipping
		}
	case insightsapi.Top:
		rows = slices.Clone(reg.All)
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].SpendPaise != rows[j].SpendPaise {
				return rows[i].SpendPaise > rows[j].SpendPaise
			}
			return rows[i].Ckey < rows[j].Ckey
		})
	default:
		rows = slices.Clone(reg.All)
		sort.SliceStable(rows, func(i, j int) bool {
			if !rows[i].LastAt.Equal(rows[j].LastAt) {
				return rows[i].LastAt.After(rows[j].LastAt)
			}
			return rows[i].Ckey < rows[j].Ckey
		})
	}
	from, to, next := window(len(rows), lim, off)
	pageRows := rows[from:to]
	items, err := s.people(ctx, q, storeID, pageRows)
	if err != nil {
		return insightsapi.StoreCustomerList{}, err
	}
	return insightsapi.StoreCustomerList{
		Segment: seg, Enough: enough, Total: len(rows),
		WindowDays: int(st.RegularWindowDays), LapseDays: int(st.RegularLapseDays),
		Items: items, NextCursor: next, ServerNow: now,
	}, nil
}

// people turns visit rows into B-09 rows: the short name of a linked
// account and the masked phone (R-16.2), never the full number.
func (s *Service) people(ctx context.Context, q *insightsdb.Queries, storeID ids.ID, rows []insightsdb.CustomerVisitsRow) ([]insightsapi.StoreCustomer, error) {
	var users []pgtype.UUID
	var hashes [][]byte
	for _, r := range rows {
		if r.CustomerID.Valid {
			users = append(users, r.CustomerID)
		} else if h, ok := strings.CutPrefix(r.Ckey, "h:"); ok {
			if b, err := hex.DecodeString(h); err == nil {
				hashes = append(hashes, b)
			}
		}
	}
	type person struct {
		name  string
		phone []byte
	}
	byUser := map[ids.ID]person{}
	if len(users) > 0 {
		ns, err := q.CustomerNames(ctx, users)
		if err != nil {
			return nil, fmt.Errorf("insights: customer names: %w", err)
		}
		for _, n := range ns {
			id, _ := ids.FromUUID(n.ID)
			byUser[id] = person{name: n.Name, phone: n.PhoneEnc}
		}
	}
	byHash := map[string][]byte{}
	if len(hashes) > 0 {
		ls, err := q.PhoneLinks(ctx, insightsdb.PhoneLinksParams{StoreID: ids.UUID(storeID), Hashes: hashes})
		if err != nil {
			return nil, fmt.Errorf("insights: phone links: %w", err)
		}
		for _, l := range ls {
			byHash["h:"+hex.EncodeToString(l.LinkPhoneHash)] = l.LinkPhoneEnc
		}
	}
	out := make([]insightsapi.StoreCustomer, 0, len(rows))
	for _, r := range rows {
		c := insightsapi.StoreCustomer{
			Visits: int(r.Visits), Bills: int(r.Bills), SpendPaise: int(r.SpendPaise), LastVisitAt: r.LastAt,
		}
		sealed := byHash[r.Ckey]
		if id, ok := ids.FromUUID(r.CustomerID); ok {
			sid := id.String()
			c.CustomerId = &sid
			p := byUser[id]
			if n := shortName(p.name); n != "" {
				c.Name = &n
			}
			sealed = p.phone
		}
		c.PhoneMasked = s.mask(ctx, sealed)
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) mask(ctx context.Context, sealed []byte) *string {
	if s.seal == nil || len(sealed) == 0 {
		return nil
	}
	plain, err := s.seal.Open(phoneField, sealed)
	if err != nil {
		// The error never carries the value; the row shows no phone.
		telemetry.Logger(ctx).WarnContext(ctx, "insights: a sealed phone did not open")
		return nil
	}
	m := pii.MaskPhone(string(plain))
	return &m
}
