package credits

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	businesscreditsapi "github.com/todayz/backend/gen/api/business-credits"
	walletapi "github.com/todayz/backend/gen/api/wallet"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// WalletOps and AccountOps map the wallet and business-credits operations
// to their authz actions, keyed as the generated strict servers name them.
var (
	WalletOps = map[string]string{
		"GetMyWallet":              policy.MeWalletView,
		"ListMyWalletTransactions": policy.MeWalletView,
	}
	AccountOps = map[string]string{
		"GetBusinessCreditAccount": policy.CreditAccountView,
	}
)

// Register mounts the wallet and business credit-account reads.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	walletapi.Register(r, walletHandlers{s},
		walletapi.StrictMiddlewareFunc(authz.Strict[walletapi.StrictHandlerFunc](az, WalletOps)))
	businesscreditsapi.Register(r, accountHandlers{s},
		businesscreditsapi.StrictMiddlewareFunc(authz.Strict[businesscreditsapi.StrictHandlerFunc](az, AccountOps)))
}

type walletHandlers struct{ s *Service }

var _ walletapi.StrictServerInterface = walletHandlers{}

func customer(ctx context.Context) (ids.ID, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return ids.Zero, problem.New(genproblems.Unauthorized, "")
	}
	return p.UserID, nil
}

// GetMyWallet implements GET /v1/me/wallet.
func (h walletHandlers) GetMyWallet(ctx context.Context, _ walletapi.GetMyWalletRequestObject) (walletapi.GetMyWalletResponseObject, error) {
	c, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	w, err := h.s.Wallet(ctx, c)
	if err != nil {
		return nil, asProblem(err)
	}
	return walletapi.GetMyWallet200JSONResponse(w), nil
}

// ListMyWalletTransactions implements GET /v1/me/wallet/transactions.
func (h walletHandlers) ListMyWalletTransactions(ctx context.Context, req walletapi.ListMyWalletTransactionsRequestObject) (walletapi.ListMyWalletTransactionsResponseObject, error) {
	c, err := customer(ctx)
	if err != nil {
		return nil, err
	}
	l, err := h.s.WalletLines(ctx, c, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return walletapi.ListMyWalletTransactions200JSONResponse(l), nil
}

type accountHandlers struct{ s *Service }

var _ businesscreditsapi.StrictServerInterface = accountHandlers{}

// GetBusinessCreditAccount implements GET /v1/businesses/{bid}/credit-account.
// The business is the one the route's check passed.
//
// TODO(lead): a manager limited to some stores sees every store of the
// business here; 04 does not say whether the account view is per store.
func (h accountHandlers) GetBusinessCreditAccount(ctx context.Context, req businesscreditsapi.GetBusinessCreditAccountRequestObject) (businesscreditsapi.GetBusinessCreditAccountResponseObject, error) {
	if _, err := customer(ctx); err != nil {
		return nil, err
	}
	d, ok := authz.DecisionFrom(ctx)
	if !ok || d.Scope.BusinessID == ids.Zero {
		return nil, problem.Wrap(genproblems.Internal, errors.New("credits: no business in the authz decision"))
	}
	a, err := h.s.CreditAccount(ctx, d.Scope.BusinessID, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return businesscreditsapi.GetBusinessCreditAccount200JSONResponse(a), nil
}
