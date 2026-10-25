package voice

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"slices"

	"github.com/go-chi/chi/v5"

	voiceapi "github.com/todayz/backend/gen/api/voice"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps the voice operations to their route check, keyed by the generated method name.
// Ownership of a draft and each line's own permission are checked in the service.
var Ops = map[string]string{
	"GetVoiceStockAvailability": policy.VoiceDraft,
	"CreateVoiceStockDraft":     policy.VoiceDraft,
	"GetVoiceStockDraft":        policy.VoiceDraft,
	"ConfirmVoiceStockDraft":    policy.VoiceDraft,
}

// Languages are the VoiceStockLanguage values, in the picker's order.
var Languages = []string{"en-IN", "hi-IN", "kn-IN", "te-IN", "ta-IN"}

// MediaTypes are the recordings the contract takes.
var MediaTypes = []string{"audio/webm", "audio/ogg", "audio/wav", "audio/mpeg", "audio/mp4"}

// Register mounts the voice operations behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	voiceapi.Register(r, handlers{s}, voiceapi.StrictMiddlewareFunc(authz.Strict[voiceapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var _ voiceapi.StrictServerInterface = handlers{}

// staffScope is the business and store the route's check passed, and the caller.
func staffScope(ctx context.Context) (authz.Scope, authn.Principal, error) {
	d, ok := authz.DecisionFrom(ctx)
	p, signed := authn.FromContext(ctx)
	if !ok || d.Scope.StoreID.IsZero() || !signed || p.IsAnonymous() {
		return authz.Scope{}, authn.Principal{}, problem.Wrap(genproblems.Internal, errors.New("voice: no store or caller in the authz decision"))
	}
	return d.Scope, p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, notFound()
	}
	return id, nil
}

// GetVoiceStockAvailability is 200 while voice update is on, else the common 404 (guard.Require).
func (h handlers) GetVoiceStockAvailability(ctx context.Context, _ voiceapi.GetVoiceStockAvailabilityRequestObject) (voiceapi.GetVoiceStockAvailabilityResponseObject, error) {
	if _, _, err := staffScope(ctx); err != nil {
		return nil, err
	}
	if err := h.s.d.Guard.Require(ctx, Feature); err != nil {
		return nil, err
	}
	langs := make([]voiceapi.VoiceStockLanguage, 0, len(Languages))
	for _, l := range Languages {
		langs = append(langs, voiceapi.VoiceStockLanguage(l))
	}
	return voiceapi.GetVoiceStockAvailability200JSONResponse{MaxSeconds: MaxSeconds, MaxBytes: MaxAudioBytes, Languages: langs}, nil
}

func (h handlers) CreateVoiceStockDraft(ctx context.Context, req voiceapi.CreateVoiceStockDraftRequestObject) (voiceapi.CreateVoiceStockDraftResponseObject, error) {
	sc, p, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	rec, err := readRecording(req.Body)
	if err != nil {
		return nil, err
	}
	id, err := h.s.Create(ctx, sc, p.UserID, rec)
	if err != nil {
		return nil, err
	}
	return voiceapi.CreateVoiceStockDraft202JSONResponse{DraftId: id.String(), Status: voiceapi.VoiceStockDraftCreatedStatusProcessing}, nil
}

// readRecording reads the form: one audio part of an allowed type and at most MaxAudioBytes,
// and an optional language. Any other part, or a second one, is refused.
func readRecording(mr *multipart.Reader) (Recording, error) {
	var rec Recording
	seen := map[string]bool{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Recording{}, problem.New(genproblems.BadRequest, "")
		}
		name := part.FormName()
		if seen[name] {
			return Recording{}, fieldErr("body."+name, "duplicate")
		}
		seen[name] = true
		switch name {
		case "audio":
			mt, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil || !slices.Contains(MediaTypes, mt) {
				return Recording{}, fieldErr("body.audio", "media_type")
			}
			b, err := io.ReadAll(io.LimitReader(part, MaxAudioBytes+1))
			if err != nil {
				return Recording{}, problem.New(genproblems.BadRequest, "")
			}
			if len(b) > MaxAudioBytes {
				return Recording{}, problem.New(genproblems.PayloadTooLarge, "")
			}
			if len(b) == 0 {
				return Recording{}, fieldErr("body.audio", "required")
			}
			rec.Data, rec.MediaType = b, mt
		case "language":
			b, err := io.ReadAll(io.LimitReader(part, 17))
			if err != nil {
				return Recording{}, problem.New(genproblems.BadRequest, "")
			}
			if l := voiceapi.VoiceStockLanguage(b); !l.Valid() {
				return Recording{}, fieldErr("body.language", "enum")
			}
			rec.Language = string(b)
		default:
			return Recording{}, fieldErr("body", "additional_property")
		}
	}
	if rec.Data == nil {
		return Recording{}, fieldErr("body.audio", "required")
	}
	return rec, nil
}

func (h handlers) GetVoiceStockDraft(ctx context.Context, req voiceapi.GetVoiceStockDraftRequestObject) (voiceapi.GetVoiceStockDraftResponseObject, error) {
	sc, p, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.DraftId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.Get(ctx, sc, p, id)
	if err != nil {
		return nil, err
	}
	return voiceapi.GetVoiceStockDraft200JSONResponse(out), nil
}

func (h handlers) ConfirmVoiceStockDraft(ctx context.Context, req voiceapi.ConfirmVoiceStockDraftRequestObject) (voiceapi.ConfirmVoiceStockDraftResponseObject, error) {
	sc, p, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	id, err := pathID(req.DraftId)
	if err != nil {
		return nil, err
	}
	res, err := h.s.Confirm(ctx, sc, p, id, req.Body.Lines)
	if err != nil {
		return nil, err
	}
	out := voiceapi.ConfirmVoiceStockDraft200JSONResponse{
		DraftId: res.DraftID.String(), Status: voiceapi.VoiceStockConfirmResultStatus(res.Status),
		Lines: make([]voiceapi.VoiceStockLineResult, 0, len(res.Lines)),
	}
	for _, l := range res.Lines {
		out.Lines = append(out.Lines, voiceapi.VoiceStockLineResult{LineId: l.LineID, Outcome: voiceapi.VoiceStockLineResultOutcome(l.Outcome)})
	}
	return out, nil
}
