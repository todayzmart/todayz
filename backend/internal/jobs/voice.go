package jobs

import (
	"context"
	"time"

	"github.com/todayz/backend/internal/platform/db"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/voice"
)

// KindVoiceTranscriptRetention removes voice draft transcripts after
// voice.TranscriptRetention (maintenance class, 02 §8 step 6).
const KindVoiceTranscriptRetention = "voice_transcript_retention"

// registervoice declares the voice domain's job kinds.
func registervoice(r *Registry, d Deps) {
	store := db.New(d.Pool)
	platformjobs.Define(r, platformjobs.Kind{
		Name: KindVoiceTranscriptRetention, Owner: "W3-04", Class: platformjobs.ClassMaintenance,
		Description: "Removes the transcript and heard words from voice stock drafts 90 days after they were made",
		Schedule:    "17 * * * *", UniqueBy: platformjobs.PerTick, Timeout: 60 * time.Second, MaxAttempts: 3,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			n, err := voice.DropTranscripts(ctx, store.Q(ctx), d.Clock.Now())
			if err != nil {
				return platformjobs.Result{}, err
			}
			return platformjobs.Result{Summary: map[string]int64{"drafts": n}}, nil
		},
	})
}
