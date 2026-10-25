package querynorm

import (
	"context"

	aiapi "github.com/todayz/backend/gen/api/ai"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps the search assist operation to its route check.
var Ops = map[string]string{"SearchAssist": policy.MeAiUse}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

// Customer is the signed-in caller (401 when anonymous).
func Customer(ctx context.Context) (ids.ID, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return ids.Zero, problem.New(genproblems.Unauthorized, "")
	}
	return p.UserID, nil
}

// APIChips are chips as the contract's AiSearchChip.
func APIChips(cs []Chip) []aiapi.AiSearchChip {
	out := make([]aiapi.AiSearchChip, 0, len(cs))
	for _, c := range cs {
		ch := aiapi.AiSearchChip{Kind: aiapi.AiSearchChipKind(c.Kind), Text: c.Text}
		if c.Value != "" {
			v := c.Value
			ch.Value = &v
		}
		out = append(out, ch)
	}
	return out
}

// Handlers are the search assist operation of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) SearchAssist(ctx context.Context, req aiapi.SearchAssistRequestObject) (aiapi.SearchAssistResponseObject, error) {
	who, err := Customer(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	sg, err := h.S.Assist(ctx, who, req.Body.Q)
	if err != nil {
		return nil, err
	}
	if sg.Empty() {
		return aiapi.SearchAssist204Response{}, nil
	}
	res := aiapi.SearchAssistResult{Chips: APIChips(sg.Chips)}
	if sg.Query != "" {
		q := sg.Query
		res.Query = &q
	}
	return aiapi.SearchAssist200JSONResponse(res), nil
}
