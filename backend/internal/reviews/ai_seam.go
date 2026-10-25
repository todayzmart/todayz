package reviews

import (
	"context"

	reviewsapi "github.com/todayz/backend/gen/api/reviews"
	"github.com/todayz/backend/internal/platform/ids"
)

// Reply posts the store's one reply to a review, exactly as POST .../reviews/{id}/reply does
// (same cleaning, same ReviewReplyExists). The review reply draft (W3-11) calls it from its
// confirm, inside the draft's transaction.
func (s *Service) Reply(ctx context.Context, userID, storeID, reviewID ids.ID, text string) (reviewsapi.ReviewReply, error) {
	rp, err := s.reply(ctx, userID, storeID, reviewID, text)
	if err != nil {
		return reviewsapi.ReviewReply{}, asProblem(err)
	}
	return rp, nil
}
