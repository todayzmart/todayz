package voicesearch

import (
	"context"
	"errors"

	aiapi "github.com/todayz/backend/gen/api/ai"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/querynorm"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/voice"
)

// Ops maps the voice search operation to its route check.
var Ops = map[string]string{"VoiceSearch": policy.MeAiUse}

// Handlers are the voice search operation of the ai domain's strict server.
type Handlers struct{ S *Service }

func (h Handlers) VoiceSearch(ctx context.Context, req aiapi.VoiceSearchRequestObject) (aiapi.VoiceSearchResponseObject, error) {
	who, err := querynorm.Customer(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.S.d.Guard.Require(ctx, Feature); err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.ValidationFailed, "").WithField("body.audio", "required")
	}
	rec, err := voice.ReadRecording(req.Body)
	if err != nil {
		return nil, err
	}
	res, err := h.S.Search(ctx, who, rec)
	if errors.Is(err, ErrUnavailable) {
		return nil, problem.New(genproblems.ServiceUnavailable, "")
	}
	if err != nil {
		return nil, err
	}
	return aiapi.VoiceSearch200JSONResponse{
		Transcript: res.Transcript,
		Language:   aiapi.AiLanguage(res.Language),
		Chips:      querynorm.APIChips(res.Chips),
	}, nil
}
