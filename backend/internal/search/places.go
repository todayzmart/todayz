package search

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Places (01 §11 location picker): PIN codes, localities and cities. The
// weekend slice has a curated set for Bengaluru, Hyderabad and Chennai
// (migration 10601); the all-India import replaces it.

const (
	defaultPlaceLimit = 10
	pincodeDigits     = 6
	// The reverse geocode answers the nearest locality within
	// reverseLocalityM, else the nearest city within reverseCityM, else 404.
	// TODO(lead): not product rules; chosen for the curated set, where a
	// locality centroid is rarely more than a few km from any point in it.
	reverseLocalityM = 5000
	reverseCityM     = 50000 //settings:ok reverse-geocode distance, not money
)

// placeRow is the common shape of the place queries.
type placeRow struct {
	ID      pgtype.UUID
	Kind    string
	Name    string
	Pincode *string
	City    *string
	Lat     float64
	Lng     float64
}

func placeOut(r placeRow) (discoverapi.DiscoverPlace, bool) {
	id, ok := ids.FromUUID(r.ID)
	if !ok {
		return discoverapi.DiscoverPlace{}, false
	}
	return discoverapi.DiscoverPlace{
		Id: id.String(), Kind: discoverapi.DiscoverPlaceKind(r.Kind), Name: r.Name,
		Pincode: r.Pincode, City: r.City, Label: placeLabel(r), Lat: r.Lat, Lng: r.Lng,
	}, true
}

// placeLabel is the location bar text: "Indiranagar, Bengaluru 560038" for a
// locality, "560038, Bengaluru" for a PIN code, "Bengaluru" for a city.
func placeLabel(r placeRow) string {
	city, pin := "", ""
	if r.City != nil {
		city = *r.City
	}
	if r.Pincode != nil {
		pin = *r.Pincode
	}
	switch r.Kind {
	case "pincode":
		return joinNonEmpty(", ", pin, city)
	case "locality":
		return joinNonEmpty(", ", r.Name, joinNonEmpty(" ", city, pin))
	default:
		return r.Name
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// SearchPlaces answers GET /v1/places/search: digits are a PIN code prefix
// (Indic digits too), anything else a locality or city name (contains, or
// close to it for typos; prefixes first).
func (s *Service) SearchPlaces(ctx context.Context, p discoverapi.SearchPlacesParams) (discoverapi.DiscoverPlaceList, error) {
	out := discoverapi.DiscoverPlaceList{Items: []discoverapi.DiscoverPlace{}}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	lim := i32(pageLimit(p.Limit, defaultPlaceLimit))
	text := typedText(p.Q)
	if text == "" {
		return out, nil
	}
	var rows []placeRow
	if digits := strings.ReplaceAll(Normalize(text), " ", ""); isDigits(digits) {
		if len(digits) > pincodeDigits {
			return out, nil
		}
		rs, err := q.PlacesByPincode(ctx, searchdb.PlacesByPincodeParams{Prefix: digits, Lim: lim})
		if err != nil {
			return out, internal(err)
		}
		for _, r := range rs {
			rows = append(rows, placeRow(r))
		}
	} else {
		esc := escapeLike(text)
		rs, err := q.PlacesByName(ctx, searchdb.PlacesByNameParams{
			Pattern: "%" + esc + "%", Q: text, PrefixPattern: esc + "%", Lim: lim,
		})
		if err != nil {
			return out, internal(err)
		}
		for _, r := range rs {
			rows = append(rows, placeRow(r))
		}
	}
	for _, r := range rows {
		if pl, ok := placeOut(r); ok {
			out.Items = append(out.Items, pl)
		}
	}
	return out, nil
}

// ReversePlace answers GET /v1/places/reverse: the nearest locality, else the
// nearest city, else 404 (outside the cities TodayZ has places for).
func (s *Service) ReversePlace(ctx context.Context, p discoverapi.ReversePlaceParams) (discoverapi.DiscoverPlace, error) {
	q, err := s.ready(ctx)
	if err != nil {
		return discoverapi.DiscoverPlace{}, err
	}
	for _, try := range []struct {
		kind string
		maxM float64
	}{{"locality", reverseLocalityM}, {"city", reverseCityM}} {
		r, err := q.NearestPlace(ctx, searchdb.NearestPlaceParams{Lng: p.Lng, Lat: p.Lat, Kind: try.kind, MaxM: try.maxM})
		if db.IsNoRows(err) {
			continue
		}
		if err != nil {
			return discoverapi.DiscoverPlace{}, internal(err)
		}
		pl, ok := placeOut(placeRow{ID: r.ID, Kind: r.Kind, Name: r.Name, Pincode: r.Pincode, City: r.City, Lat: r.Lat, Lng: r.Lng})
		if ok {
			return pl, nil
		}
	}
	return discoverapi.DiscoverPlace{}, problem.New(genproblems.NotFound, "No place is known near this point yet.")
}
