package policy

import "github.com/todayz/backend/internal/platform/authz"

// Business actions (W1-04, 02 §3.3 "Business" rows). standee.manage,
// inbox.view and support.tickets are business-domain rows of the matrix that
// arrive with their own work packages; asks.view and asks.reply come with
// W3-16. asks.manage is here because the store settings carry asksMuted.
const (
	// KindBusinessUser: any signed-in business-app user, no business scope.
	BusinessList      = "business.list"
	BusinessCreate    = "business.create"
	StaffAcceptInvite = "staff.accept_invite"

	// KindBusiness: by role in the path's business ({bid}) and store ({sid}).
	BusinessView        = "business.view"
	BusinessEdit        = "business.edit"
	BusinessSubmit      = "business.submit"
	KycView             = "kyc.view"
	KycEdit             = "kyc.edit"
	StaffView           = "staff.view"
	StaffManage         = "staff.manage"
	StaffManageCashiers = "staff.manage_cashiers"
	StoreManage         = "store.manage"
	StoreSettingsEdit   = "store.settings.edit"
	AsksManage          = "asks.manage"
)

func init() {
	owner := map[authz.Role]authz.Grant{authz.Owner: authz.Allow}
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "business", Actions: []authz.Action{
		{Name: BusinessList, Kind: authz.KindBusinessUser, Doc: "GET /v1/biz/businesses: my businesses and pending invitations"},
		{Name: BusinessCreate, Kind: authz.KindBusinessUser, Doc: "POST /v1/biz/businesses: start an onboarding draft; the caller becomes its owner"},
		{Name: StaffAcceptInvite, Kind: authz.KindBusinessUser, Doc: "POST {B}/staff/{staffId}/accept: the invited phone joins (the handler matches the caller's phone)"},

		{Name: BusinessView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {B}, documents list"},
		{Name: BusinessEdit, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "PATCH {B} (onboarding draft)"},
		{Name: BusinessSubmit, Kind: authz.KindBusiness, Grants: owner, Doc: "POST {B}/submit"},
		{Name: KycView, Kind: authz.KindBusiness, Grants: map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Masked}, Doc: "GET {B}/kyc (manager: status and last 4 only)"},
		{Name: KycEdit, Kind: authz.KindBusiness, Grants: owner, Doc: "PUT {B}/kyc, POST {B}/documents"},
		{Name: StaffView, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {B}/staff"},
		{Name: StaffManage, Kind: authz.KindBusiness, Grants: owner, Doc: "invite or remove a manager, PATCH {B}/staff/{staffId}"},
		{Name: StaffManageCashiers, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {B}/staff (cashier), DELETE {B}/staff/{staffId} (cashier)"},
		{Name: StoreManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "stores: list, create, GET/PATCH {S}, POST {S}/submit"},
		{Name: StoreSettingsEdit, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET/PATCH {S}/settings"},
		{Name: AsksManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "PATCH {S}/settings asksMuted (W3-16 asks)"},
	}})
}
