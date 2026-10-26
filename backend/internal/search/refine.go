package search

import (
	"context"
	"strconv"
	"strings"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// RefineChip is one refine chip read from a query (01 §11.1, D-101). Kind is a
// DiscoverChipKind; Value is the paise for maxPrice and the brand slug for brand, "" for the
// others; Text is the query's words it came from.
type RefineChip struct{ Kind, Value, Text string }

// ChipReader reads the refine chips out of a query and answers the words left to search for.
// cmd/todayz wires the deterministic parser of internal/ai/querynorm (no model, W3-12); nil
// reads no chips.
type ChipReader interface {
	ReadChips(ctx context.Context, q string) (rest string, chips []RefineChip, err error)
}

// refine reads the chips of req.Q unless the customer turned parsing off (parse=false, after
// removing a chip or tapping a search assist suggestion), applies them as filters and leaves
// req.Q as the words that were not chips. A chip whose filter the request already sets
// explicitly is not applied and not shown. Reading never fails a search: on an error the
// query is searched as typed. hint is the parser half of refineHint (01 §11.1, D-231):
// parsing was on, no chip was read and the query has at least refine_ai_min_words words.
// The other half, the synonym miss (D-238), is known only after the search has run: Search
// sets it from find's result, unless req.skipMiss says there was nothing to search for
// (chips read, no words left).
func (s *Service) refine(ctx context.Context, req *searchReq) (chips []discoverapi.DiscoverChip, hint bool, err error) {
	chips = []discoverapi.DiscoverChip{}
	if !req.Parse {
		return chips, false, nil
	}
	if s.chips != nil {
		rest, read, rerr := s.chips.ReadChips(ctx, req.Q)
		switch {
		case rerr != nil:
			telemetry.Logger(ctx).WarnContext(ctx, "search: refine chips", "err", rerr)
		case len(read) > 0 && Normalize(rest) == "":
			req.skipMiss = true // only chips: searched as typed, never offered to the model
		case len(read) > 0:
			req.Q = rest
			for _, c := range read {
				if applyChip(req, c) {
					dc := discoverapi.DiscoverChip{Kind: discoverapi.DiscoverChipKind(c.Kind), Text: c.Text}
					if c.Value != "" {
						v := c.Value
						dc.Value = &v
					}
					chips = append(chips, dc)
				}
			}
		}
	}
	if len(chips) > 0 {
		return chips, false, nil
	}
	minWords, err := s.settings.Int(ctx, settings.RefineAiMinWords)
	if err != nil {
		return chips, false, err
	}
	return chips, int64(len(strings.Fields(req.Q))) >= minWords, nil
}

// applyChip sets the chip's filter on the request (01 §11.1: "offers" is hasOffer, "near me"
// is Nearest first) and says whether it did.
func applyChip(req *searchReq, c RefineChip) bool {
	switch discoverapi.DiscoverChipKind(c.Kind) {
	case discoverapi.DiscoverChipKindMaxPrice:
		paise, err := strconv.ParseInt(c.Value, 10, 64)
		if err != nil || paise <= 0 || req.MaxPrice != nil {
			return false
		}
		req.MaxPrice = &paise
	case discoverapi.DiscoverChipKindOpenNow:
		req.OpenNow = true
	case discoverapi.DiscoverChipKindPickup:
		req.Pickup = true
	case discoverapi.DiscoverChipKindDelivery:
		req.Delivery = true
	case discoverapi.DiscoverChipKindOffers:
		req.HasOffer = true
	case discoverapi.DiscoverChipKindNearest:
		if req.Sort != sortRelevance {
			return false
		}
		req.Sort = sortNearest
	case discoverapi.DiscoverChipKindBrand:
		if c.Value == "" || req.Brand != "" {
			return false
		}
		req.Brand = c.Value
	default:
		return false
	}
	return true
}
