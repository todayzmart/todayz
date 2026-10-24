package pos

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP is HMAC-SHA1 (D-12); the customer apps compute the same, not a collision use
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	posapi "github.com/todayz/backend/gen/api/pos"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// The counter code (01 §6, D-12): RFC 6238 TOTP with SHA-1, 60 s steps and
// 4 digits, checked at the step of the moment and one step either side.
const (
	totpPeriod    = 60 * time.Second
	totpDigits    = 4
	totpDrift     = 1  // steps either side
	totpSecretLen = 20 // bytes (160 bits, RFC 4226 §4)
	// maxClockOffset is the most a device's clock may be off at its last
	// online check for an offline code to count (sync.yaml SyncSaleLink).
	maxClockOffset = 2 * time.Minute

	secretField = "counter_secret" // the pii.Sealer field label
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpStep is the 60 s step t falls in.
func totpStep(t time.Time) int64 { return t.Unix() / int64(totpPeriod/time.Second) }

// totp is the 4-digit code for secret at step (RFC 4226 §5.3 truncation).
func totp(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) // #nosec G115 -- steps after 1970 are positive
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	h := m.Sum(nil)
	off := h[len(h)-1] & 0x0f
	bin := binary.BigEndian.Uint32(h[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, bin%10_000) //settings:ok 10^totpDigits, the code space, not a money amount
}

// codeAt finds the step within ±1 of at whose code is code, 0 when none.
func codeAt(secret []byte, code string, at time.Time) int64 {
	base := totpStep(at)
	var found int64
	for d := int64(-totpDrift); d <= totpDrift; d++ {
		// Check every step (no early exit) so the time does not depend on
		// which step matched.
		if subtle.ConstantTimeCompare([]byte(totp(secret, base+d)), []byte(code)) == 1 && found == 0 {
			found = base + d
		}
	}
	return found
}

// codeMatch is the customer's device and step a code is right for.
type codeMatch struct {
	customerID, deviceID ids.ID
	name                 string
	step                 int64
}

// matchCode checks code at the time at against every device of the account
// with the phone. ok is false for a wrong code or a phone with no account
// (the caller cannot tell which, D-12).
func (s *Service) matchCode(ctx context.Context, phone, code string, at time.Time) (codeMatch, bool, error) {
	e164, ok := pii.NormalizePhone(phone)
	if !ok {
		return codeMatch{}, false, nil
	}
	q := s.q(ctx)
	u, err := q.UserByPhoneHash(ctx, s.hasher.Phone(e164))
	if isNoRows(err) {
		return codeMatch{}, false, nil
	}
	if err != nil {
		return codeMatch{}, false, fmt.Errorf("pos: user by phone: %w", err)
	}
	devs, err := q.CounterDevices(ctx, u.ID)
	if err != nil {
		return codeMatch{}, false, fmt.Errorf("pos: counter devices: %w", err)
	}
	for _, d := range devs {
		secret, err := s.devices.Open(secretField, d.CounterSecretEnc)
		if err != nil {
			// A secret sealed under another key cannot match; never log it.
			continue
		}
		if step := codeAt(secret, code, at); step > 0 {
			return codeMatch{customerID: mustID(u.ID), deviceID: mustID(d.ID), name: u.Name, step: step}, true, nil
		}
	}
	return codeMatch{}, false, nil
}

// codeGuard is who a counter-code check counts against (D-131): the phone
// at the store and the cashier. phone is nil when it does not parse.
type codeGuard struct {
	store, cashier ids.ID
	phone          []byte
}

func (s *Service) guard(store, cashier ids.ID, phone string) codeGuard {
	g := codeGuard{store: store, cashier: cashier}
	if e164, ok := pii.NormalizePhone(phone); ok {
		g.phone = s.hasher.Phone(e164)
	}
	return g
}

// codeLocks serialises the guard's counts until commit (the cashier, then
// the phone, always in that order) and reads them.
func (s *Service) codeLocks(ctx context.Context, g codeGuard, now time.Time) (posdb.CodeLocksRow, error) {
	q := s.q(ctx)
	keys := []string{"pos.code.cashier:" + g.cashier.String()}
	if g.phone != nil {
		keys = append(keys, "pos.code.phone:"+g.store.String()+":"+hex.EncodeToString(g.phone))
	}
	for _, k := range keys {
		if err := q.LockCodeKey(ctx, k); err != nil {
			return posdb.CodeLocksRow{}, fmt.Errorf("pos: lock code key: %w", err)
		}
	}
	r, err := q.CodeLocks(ctx, posdb.CodeLocksParams{
		StoreID: ids.UUID(g.store), PhoneHash: g.phone, CashierUserID: ids.UUID(g.cashier), Now: now,
		PhoneLockSince: now.Add(-phoneLock), CashierLockSince: now.Add(-cashierLock),
		PhoneWindowSince: now.Add(-phoneFailWindow), CashierWindowSince: now.Add(-cashierFailWindow),
	})
	if err != nil {
		return r, fmt.Errorf("pos: code locks: %w", err)
	}
	return r, nil
}

func codeLocked(r posdb.CodeLocksRow) bool { return r.PhoneLocked || r.CashierLocked }

// codeFailed records a wrong code against the guard (source online or
// sync); the failure that reaches a limit locks the phone or the cashier,
// and locked reports that it did.
func (s *Service) codeFailed(ctx context.Context, g codeGuard, r posdb.CodeLocksRow, source string, now time.Time) (locked bool, err error) {
	var phoneUntil, cashierUntil *time.Time
	if g.phone != nil && int(r.PhoneFailures)+1 >= phoneFailMax {
		phoneUntil = ptr(now.Add(phoneLock))
	}
	if int(r.CashierFailures)+1 >= cashierFailMax {
		cashierUntil = ptr(now.Add(cashierLock))
	}
	err = s.q(ctx).InsertCodeFailure(ctx, posdb.InsertCodeFailureParams{
		ID: ids.UUID(s.ids.New(now)), StoreID: ids.UUID(g.store), CashierUserID: ids.UUID(g.cashier),
		PhoneHash: g.phone, Source: source, PhoneLockedUntil: phoneUntil, CashierLockedUntil: cashierUntil, Now: now,
	})
	if err != nil {
		return false, fmt.Errorf("pos: code failure: %w", err)
	}
	return phoneUntil != nil || cashierUntil != nil, nil
}

// codeReady also needs the device and phone keys.
func (s *Service) codeReady(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.devices == nil || s.hasher == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

// --- the customer's secret ---------------------------------------------------

// Seed is the signed-in device's counter-code secret, made on the first
// call (GET /v1/me/counter-code-seed).
func (s *Service) Seed(ctx context.Context) (posapi.CounterCodeSeed, error) {
	if err := s.codeReady(ctx); err != nil {
		return posapi.CounterCodeSeed{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.CounterCodeSeed{}, err
	}
	dev, real, err := s.device(ctx, p)
	if err != nil {
		return posapi.CounterCodeSeed{}, asProblem(err)
	}
	if !real {
		return posapi.CounterCodeSeed{}, problem.New(genproblems.Conflict, "")
	}
	var secret []byte
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		d, err := q.LockDevice(ctx, ids.UUID(dev))
		if isNoRows(err) || (err == nil && mustID(d.UserID) != p.UserID) {
			return problem.New(genproblems.Conflict, "")
		}
		if err != nil {
			return fmt.Errorf("pos: lock device: %w", err)
		}
		if d.CounterSecretEnc != nil {
			secret, err = s.devices.Open(secretField, d.CounterSecretEnc)
			if err == nil {
				return nil
			}
			// TODO(lead): a secret sealed under a rotated DEVICE_SECRET_ENC_KEY
			// cannot be opened; the device needs a reset path (not in the plan).
			return fmt.Errorf("pos: open counter secret: %w", err)
		}
		if secret, err = s.random(totpSecretLen); err != nil {
			return err
		}
		enc, err := s.devices.Seal(secretField, secret)
		if err != nil {
			return fmt.Errorf("pos: seal counter secret: %w", err)
		}
		if err := q.SetCounterSecret(ctx, posdb.SetCounterSecretParams{SecretEnc: enc, ID: d.ID}); err != nil {
			return fmt.Errorf("pos: set counter secret: %w", err)
		}
		return nil
	})
	if err != nil {
		return posapi.CounterCodeSeed{}, asProblem(err)
	}
	return posapi.CounterCodeSeed{
		Algorithm: posapi.SHA1, Digits: posapi.N4, PeriodSeconds: posapi.N60,
		Secret: b32.EncodeToString(secret), ServerNow: s.now(),
	}, nil
}

// --- the POS check -----------------------------------------------------------

// VerifyCode checks a customer's counter code for a bill
// (POST {S}/counter-code/verify). Five wrong codes lock the bill, five in
// 15 minutes lock the phone at the store and twenty in an hour lock the
// cashier (D-131); a wrong code's attempt is committed even though the call
// fails.
func (s *Service) VerifyCode(ctx context.Context, sc authz.Scope, body posapi.CounterCodeVerifyRequest) (posapi.CounterCodeVerification, error) {
	if err := s.codeReady(ctx); err != nil {
		return posapi.CounterCodeVerification{}, err
	}
	clientID, err := ids.Parse(body.SaleClientId)
	if err != nil {
		return posapi.CounterCodeVerification{}, fieldErr("body.saleClientId", "ulid")
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.CounterCodeVerification{}, err
	}
	g := s.guard(sc.StoreID, p.UserID, body.Phone)
	key := posdb.LockCounterCheckParams{StoreID: ids.UUID(sc.StoreID), SaleClientID: ids.UUID(clientID)}
	var out posapi.CounterCodeVerification
	var refused *problem.Error
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		now := s.now()
		locks, err := s.codeLocks(ctx, g, now)
		if err != nil {
			return err
		}
		err = q.EnsureCounterCheck(ctx, posdb.EnsureCounterCheckParams{StoreID: key.StoreID, SaleClientID: key.SaleClientID, Now: now})
		if err != nil {
			return fmt.Errorf("pos: counter check: %w", err)
		}
		c, err := q.LockCounterCheck(ctx, key)
		if err != nil {
			return fmt.Errorf("pos: lock counter check: %w", err)
		}
		if c.LockedAt != nil {
			refused = problem.New(genproblems.CodeLocked, "")
			return nil
		}
		if c.VerifiedAt != nil && now.Sub(*c.VerifiedAt) <= codeCheckTTL {
			// Already right for this bill: answer the same customer again.
			// An older check is checked again (it would not ring a sale).
			name, err := q.UserName(ctx, c.CustomerID)
			if err != nil {
				return fmt.Errorf("pos: customer name: %w", err)
			}
			out = posapi.CounterCodeVerification{CustomerName: name, SaleClientId: clientID.String()}
			return nil
		}
		if codeLocked(locks) {
			// The phone or the cashier is locked: no code is checked.
			refused = problem.New(genproblems.CodeLocked, "")
			return nil
		}
		m, ok, err := s.matchCode(ctx, body.Phone, body.Code, now)
		if err != nil {
			return err
		}
		if ok {
			// A (device, step) is accepted once: a replayed code is wrong.
			_, err := q.InsertCodeUse(ctx, posdb.InsertCodeUseParams{
				CustomerDeviceID: ids.UUID(m.deviceID), TimeStep: m.step, StoreID: key.StoreID, Now: now,
			})
			switch {
			case isNoRows(err):
				ok = false
			case err != nil:
				return fmt.Errorf("pos: code use: %w", err)
			}
		}
		if !ok {
			r, err := q.FailCounterCheck(ctx, posdb.FailCounterCheckParams{
				MaxAttempts: codeTries, Now: now, StoreID: key.StoreID, SaleClientID: key.SaleClientID,
			})
			if err != nil {
				return fmt.Errorf("pos: fail counter check: %w", err)
			}
			locked, err := s.codeFailed(ctx, g, locks, "online", now)
			if err != nil {
				return err
			}
			if r.LockedAt != nil || locked {
				refused = problem.New(genproblems.CodeLocked, "")
			} else {
				refused = problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", max(codeTries-int(r.Attempts), 0))
			}
			return nil // commit the attempt
		}
		err = q.VerifyCounterCheck(ctx, posdb.VerifyCounterCheckParams{
			CustomerID: ids.UUID(m.customerID), CustomerDeviceID: ids.UUID(m.deviceID), TimeStep: &m.step, Now: &now,
			StoreID: key.StoreID, SaleClientID: key.SaleClientID,
		})
		if err != nil {
			return fmt.Errorf("pos: verify counter check: %w", err)
		}
		out = posapi.CounterCodeVerification{CustomerName: m.name, SaleClientId: clientID.String()}
		return nil
	})
	switch {
	case err != nil:
		return posapi.CounterCodeVerification{}, asProblem(err)
	case refused != nil:
		return posapi.CounterCodeVerification{}, refused
	}
	out.ServerNow = s.now()
	return out, nil
}
