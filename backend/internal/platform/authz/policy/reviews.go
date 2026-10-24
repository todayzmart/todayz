package policy

import "github.com/todayz/backend/internal/platform/authz"

// Reviews actions (W2-06, 02 §3.3 "reviews" rows, D-41). The public shop page list
// (GET /v1/discover/stores/{slug}/reviews) is authz.Public and has no action. Cashiers
// do not see reviews at all (01 §3: replies are owner and manager).
const (
	// KindCustomer: the signed-in customer (/v1/me), on their own reviews.
	MeReviewsView   = "me.reviews.view"   // list own reviews
	MeReviewsManage = "me.reviews.manage" // write, edit, report another customer's review

	// KindBusiness: by role in {bid} with access to {sid}.
	ReviewsView  = "reviews.view"
	ReviewReply  = "review.reply"
	ReviewReport = "review.report"

	// KindAdmin: support, admin and super_admin.
	AdminReviewModerate = "admin.review.moderate" // list reviews and reports, hide, resolve a report
	AdminReviewRestore  = "admin.review.restore"  // restore a hidden review or reply, with a reason (D-41)
)

func init() {
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	moderators := map[authz.Role]authz.Grant{authz.Support: authz.Allow, authz.Admin: authz.Allow, authz.SuperAdmin: authz.Allow}
	Register(authz.Table{Domain: "reviews", Actions: []authz.Action{
		{Name: MeReviewsView, Kind: authz.KindCustomer, Doc: "GET /v1/me/reviews: own reviews only, hidden ones included"},
		{Name: MeReviewsManage, Kind: authz.KindCustomer, Doc: "POST /v1/me/reviews, PATCH /v1/me/reviews/{reviewId}, POST /v1/me/review-reports (C-14): own reviews; reporting someone else's"},

		{Name: ReviewsView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/reviews (B-14)"},
		{Name: ReviewReply, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST|PATCH {S}/reviews/{reviewId}/reply: one reply, editable"},
		{Name: ReviewReport, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {S}/reviews/{reviewId}/report"},

		{Name: AdminReviewModerate, Kind: authz.KindAdmin, Grants: moderators, Doc: "GET /v1/admin/reviews, /v1/admin/review-reports; POST .../reviews/{reviewId}/hide, .../reply/hide, /review-reports/{reportId}/resolve: audited with a reason"},
		{Name: AdminReviewRestore, Kind: authz.KindAdmin, Grants: moderators, Doc: "POST /v1/admin/reviews/{reviewId}/restore, .../reply/restore: audited with a reason (D-41)"},
	}})
}
