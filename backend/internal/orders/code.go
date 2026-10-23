package orders

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/todayz/backend/gen/sqlc/ordersdb"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The pickup code (01 §6, D-04).
const (
	codeDigits = 4
	// codeTries is how many wrong codes lock an order's code (01 §6: "5
	// wrong tries lock it"); 01 §17 has no setting for it.
	codeTries = 5 //settings:ok 01 §6 fixes the order-code lock at 5 tries; no platform setting exists
	// codeAssignTries bounds the redraws when a store's open orders already
	// use a drawn code (ux_orders_store_code_active).
	codeAssignTries = 8
	// uxStoreCode is the unique index that keeps a store's open codes apart.
	uxStoreCode = "ux_orders_store_code_active"
)

// codeSpace is 10^codeDigits, the number of distinct codes; draw picks
// uniformly below it. Derived, so the range follows the digit count.
var codeSpace = new(big.Int).Exp(big.NewInt(10), big.NewInt(codeDigits), nil)

var errNoCodeKeys = errors.New("orders: ORDER_CODE_ENC_KEY and ORDER_CODE_HMAC_KEY are required to place or re-code an order")

// codeKeys seals and looks up pickup codes.
type codeKeys struct {
	seal *pii.Sealer
	mac  *pii.Hasher
	rnd  io.Reader // crypto/rand.Reader; Deps.CodeRand replaces it in tests
}

// newCodeKeys builds the keys; nil (and no error) when either is unset, so
// the API still starts and only placing an order answers 500.
func newCodeKeys(enc, mac config.Secret) (*codeKeys, error) {
	if !enc.IsSet() || !mac.IsSet() {
		return nil, nil //nolint:nilnil // no keys is a valid state: placing refuses
	}
	s, err := pii.NewSealer(enc.Bytes())
	if err != nil {
		return nil, fmt.Errorf("orders: ORDER_CODE_ENC_KEY: %w", err)
	}
	h, err := pii.NewHasher(mac.Bytes())
	if err != nil {
		return nil, fmt.Errorf("orders: ORDER_CODE_HMAC_KEY: %w", err)
	}
	return &codeKeys{seal: s, mac: h, rnd: rand.Reader}, nil
}

// sealedCode is a code as stored. It never holds the plain code.
type sealedCode struct {
	enc, mac []byte
}

func codeField(orderID ids.ID) string { return "order_code:" + orderID.String() }

// lookup is the code's HMAC, bound to the store (the unique index is per
// store, and a code typed at one store never matches another's).
func (k *codeKeys) lookup(storeID ids.ID, code string) []byte {
	return k.mac.Sum("order_code:" + storeID.String() + ":" + code)
}

// draw makes a new random code for the order and seals it.
func (k *codeKeys) draw(storeID, orderID ids.ID) (sealedCode, error) {
	n, err := rand.Int(k.rnd, codeSpace)
	if err != nil {
		return sealedCode{}, fmt.Errorf("orders: draw code: %w", err)
	}
	code := fmt.Sprintf("%0*d", codeDigits, n.Int64())
	enc, err := k.seal.Seal(codeField(orderID), []byte(code))
	if err != nil {
		return sealedCode{}, fmt.Errorf("orders: seal code: %w", err)
	}
	return sealedCode{enc: enc, mac: k.lookup(storeID, code)}, nil
}

// open decrypts the order's code for its owner.
func (k *codeKeys) open(orderID ids.ID, enc []byte) (string, error) {
	b, err := k.seal.Open(codeField(orderID), enc)
	if err != nil {
		return "", fmt.Errorf("orders: open code: %w", err)
	}
	return string(b), nil
}

// matches compares a typed code with the order's in constant time.
func (k *codeKeys) matches(storeID ids.ID, typed string, stored []byte) bool {
	return hmac.Equal(k.lookup(storeID, typed), stored)
}

// withFreshCode draws codes until write succeeds with one no open order of
// the store uses. write runs in a savepoint, so a unique violation on the
// store's codes rolls back only that attempt.
func (s *Service) withFreshCode(ctx context.Context, storeID, orderID ids.ID, write func(ctx context.Context, c sealedCode) error) error {
	if s.codes == nil {
		return errNoCodeKeys
	}
	for range codeAssignTries {
		c, err := s.codes.draw(storeID, orderID)
		if err != nil {
			return err
		}
		taken, err := s.q(ctx).CodeInUse(ctx, ordersdb.CodeInUseParams{StoreID: ids.UUID(storeID), CodeHmac: c.mac})
		if err != nil {
			return fmt.Errorf("orders: code in use: %w", err)
		}
		if taken {
			redrawn(ctx, "in_use")
			continue
		}
		err = s.db.InTx(ctx, func(ctx context.Context) error { return write(ctx, c) })
		if db.IsUniqueViolation(err, uxStoreCode) {
			// Another order of the store took the code between the check
			// and the insert; only this attempt's savepoint rolled back.
			redrawn(ctx, "unique_violation")
			continue
		}
		return err
	}
	return errors.New("orders: no free pickup code for the store after several draws")
}

// redrawn logs one redraw of a pickup code and why, never the code. Many
// of them mean a store's open orders crowd the code space.
func redrawn(ctx context.Context, reason string) {
	telemetry.Logger(ctx).InfoContext(ctx, "orders: pickup code redrawn", "reason", reason)
}
