package business

import (
	"context"
	"fmt"
	"strings"

	businessapi "github.com/todayz/backend/gen/api/business"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// KYC statuses (03 business_kyc.status).
const (
	kycMissing   = "missing"
	kycSubmitted = "submitted"
)

// Sealing field names: the AAD that binds a sealed value to its column. The
// seed (internal/seed) seals with the same names, so the service opens what
// it wrote.
const (
	FieldPAN         = "pan"
	FieldGSTIN       = "gstin"
	FieldUPI         = "upi_id"
	FieldBankAccount = "bank_account"
	FieldPhone       = "phone"
)

// --- PAN and GSTIN -------------------------------------------------------------

// panHolderTypes are the valid 4th characters of a PAN (the holder type:
// company, person, HUF, firm, AOP, trust, BOI, local authority, artificial
// juridical person, government).
const panHolderTypes = "ABCFGHJLPT"

// ValidPAN checks a PAN's shape (5 letters, 4 digits, a letter) and its
// holder type. A PAN has no check digit.
func ValidPAN(pan string) bool {
	if len(pan) != 10 {
		return false
	}
	for i := 0; i < 10; i++ {
		c := pan[i]
		switch {
		case i < 5 || i == 9:
			if c < 'A' || c > 'Z' {
				return false
			}
		default:
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return strings.IndexByte(panHolderTypes, pan[3]) >= 0
}

const gstinAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// gstinCheck is the GSTIN check character of the first 14 characters (GSTN's
// mod-36 scheme: alternate factors 1 and 2, sum quotient and remainder).
func gstinCheck(first14 string) (byte, bool) {
	sum := 0
	for i := 0; i < 14; i++ {
		v := strings.IndexByte(gstinAlphabet, first14[i])
		if v < 0 {
			return 0, false
		}
		f := 1
		if i%2 == 1 {
			f = 2
		}
		p := v * f
		sum += p/36 + p%36
	}
	return gstinAlphabet[(36-sum%36)%36], true
}

// validGSTINState: 01–38 are the states and union territories, 97 other
// territory, 99 the centre jurisdiction.
func validGSTINState(code string) bool {
	if len(code) != 2 || code[0] < '0' || code[0] > '9' || code[1] < '0' || code[1] > '9' {
		return false
	}
	n := int(code[0]-'0')*10 + int(code[1]-'0')
	return (n >= 1 && n <= 38) || n == 97 || n == 99
}

// GSTINReason says what is wrong with a GSTIN, or "" when it is valid. pan,
// when not empty, is the business's PAN: characters 3–12 must be it.
func GSTINReason(gstin, pan string) string {
	if len(gstin) != 15 {
		return "must be a GSTIN"
	}
	if !validGSTINState(gstin[:2]) {
		return "must start with a state code"
	}
	if !ValidPAN(gstin[2:12]) || gstin[13] != 'Z' {
		return "must be a GSTIN"
	}
	c, ok := gstinCheck(gstin[:14])
	if !ok || c != gstin[14] {
		return "check digit does not match"
	}
	if pan != "" && gstin[2:12] != pan {
		return "must contain the PAN"
	}
	return ""
}

// --- view ----------------------------------------------------------------------

func toKyc(k businessdb.BusinessKyc, grant authz.Grant) businessapi.Kyc {
	out := businessapi.Kyc{
		Status:           businessapi.KycStatus(k.Status),
		PanLast4:         k.PanLast4,
		GstinLast4:       k.GstinLast4,
		UpiIdLast4:       k.UpiIDLast4,
		BankAccountLast4: k.BankAccountLast4,
	}
	if grant == authz.Allow {
		out.BankIfsc, out.LegalName, out.VerifiedAt = k.BankIfsc, k.LegalName, k.VerifiedAt
	}
	return out
}

// kycRow reads the business's KYC row, creating the empty one a business
// made before W1-04 (or by hand) may lack.
func (s *Service) kycRow(ctx context.Context, bid ids.ID) (businessdb.BusinessKyc, error) {
	q := s.q(ctx)
	k, err := q.GetKyc(ctx, ids.UUID(bid))
	if db.IsNoRows(err) {
		if err := q.InsertKyc(ctx, businessdb.InsertKycParams{BusinessID: ids.UUID(bid), Now: s.clock.Now()}); err != nil {
			return businessdb.BusinessKyc{}, fmt.Errorf("business: kyc row: %w", err)
		}
		k, err = q.GetKyc(ctx, ids.UUID(bid))
	}
	if err != nil {
		return businessdb.BusinessKyc{}, fmt.Errorf("business: kyc: %w", err)
	}
	return k, nil
}

func (s *Service) getKyc(ctx context.Context, bidStr string) (businessapi.Kyc, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Kyc{}, 0, err
	}
	dec, err := decision(ctx)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	k, err := s.kycRow(ctx, bid)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	return toKyc(k, dec.Grant), k.Version, nil
}

// --- update --------------------------------------------------------------------

// sealedField is one KYC value after a merge: sealed bytes and last 4.
type sealedField struct {
	enc   []byte
	last4 *string
}

func last4(s string) *string {
	if len(s) > 4 {
		s = s[len(s)-4:]
	}
	return &s
}

// upiLast4 is the last (up to) 4 characters of a UPI id's name part: the
// handle after @ is the bank, not the payee.
func upiLast4(upi string) *string {
	name, _, _ := strings.Cut(upi, "@")
	return last4(name)
}

// putKyc changes the KYC details. A field left out keeps its value, null
// clears it. Every change sends the details back to submitted (or missing
// without a PAN) and is audited by field name only.
func (s *Service) putKyc(ctx context.Context, bidStr string, ifMatchHdr *string, body *businessapi.KycUpdate) (businessapi.Kyc, int32, error) {
	if err := s.ready(ctx); err != nil {
		return businessapi.Kyc{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	dec, err := decision(ctx)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	bid, err := pathID(bidStr)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	version, err := ifMatch(ifMatchHdr)
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	if body == nil {
		return businessapi.Kyc{}, 0, problem.New(genproblems.BadRequest, "")
	}

	var out businessdb.BusinessKyc
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.kycRow(ctx, bid)
		if err != nil {
			return err
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		params, changed, err := s.mergeKyc(ctx, cur, body)
		if err != nil {
			return err
		}
		params.BusinessID, params.Version = cur.BusinessID, version
		out, err = s.q(ctx).UpdateKyc(ctx, params)
		if db.IsNoRows(err) {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if err != nil {
			return err
		}
		return s.writeAudit(ctx, p.UserID, "kyc.update", "business", bid.String(),
			map[string]any{"status": cur.Status}, map[string]any{"status": out.Status, "fields": changed})
	})
	if err != nil {
		return businessapi.Kyc{}, 0, err
	}
	return toKyc(out, dec.Grant), out.Version, nil
}

// mergeKyc applies the body to the current row: validates, seals and works
// out the last 4s and the status. changed lists the field names sent.
func (s *Service) mergeKyc(ctx context.Context, cur businessdb.BusinessKyc, body *businessapi.KycUpdate) (businessdb.UpdateKycParams, []string, error) {
	var v invalid
	var changed []string
	params := businessdb.UpdateKycParams{
		PanEnc: cur.PanEnc, PanLast4: cur.PanLast4,
		GstinEnc: cur.GstinEnc, GstinLast4: cur.GstinLast4,
		UpiIDEnc: cur.UpiIDEnc, UpiIDLast4: cur.UpiIDLast4,
		BankAccountEnc: cur.BankAccountEnc, BankAccountLast4: cur.BankAccountLast4,
		BankIfsc: cur.BankIfsc, LegalName: cur.LegalName,
	}

	// The PAN after the merge, in clear, for the GSTIN check.
	pan := ""
	switch {
	case sent(ctx, "pan") && body.Pan != nil:
		pan = strings.ToUpper(*body.Pan)
		if !ValidPAN(pan) {
			v.add("body.pan", "must be a PAN")
		}
	case !sent(ctx, "pan") && cur.PanEnc != nil:
		plain, err := s.seal.Open(FieldPAN, cur.PanEnc)
		if err != nil {
			return params, nil, fmt.Errorf("business: open pan: %w", err)
		}
		pan = string(plain)
	}

	seal := func(field, value string, l4 *string) (sealedField, error) {
		enc, err := s.seal.Seal(field, []byte(value))
		if err != nil {
			return sealedField{}, fmt.Errorf("business: seal %s: %w", field, err)
		}
		return sealedField{enc: enc, last4: l4}, nil
	}

	if sent(ctx, "pan") {
		changed = append(changed, "pan")
		params.PanEnc, params.PanLast4 = nil, nil
		if body.Pan != nil && pan != "" {
			f, err := seal(FieldPAN, pan, last4(pan))
			if err != nil {
				return params, nil, err
			}
			params.PanEnc, params.PanLast4 = f.enc, f.last4
		}
	}

	gstinNow := cur.GstinEnc != nil
	if sent(ctx, "gstin") {
		changed = append(changed, "gstin")
		params.GstinEnc, params.GstinLast4 = nil, nil
		gstinNow = body.Gstin != nil
		if body.Gstin != nil {
			g := strings.ToUpper(*body.Gstin)
			if r := GSTINReason(g, pan); r != "" {
				v.add("body.gstin", r)
			} else {
				f, err := seal(FieldGSTIN, g, last4(g))
				if err != nil {
					return params, nil, err
				}
				params.GstinEnc, params.GstinLast4 = f.enc, f.last4
			}
		}
	} else if gstinNow && sent(ctx, "pan") && pan != "" {
		// The PAN changed under a kept GSTIN: they must still agree.
		plain, err := s.seal.Open(FieldGSTIN, cur.GstinEnc)
		if err != nil {
			return params, nil, fmt.Errorf("business: open gstin: %w", err)
		}
		if r := GSTINReason(string(plain), pan); r != "" {
			v.add("body.pan", "must match the GSTIN")
		}
	}
	// TODO(lead): a GSTIN needs a PAN (its characters 3–12 are the PAN);
	// the conservative reading refuses a GSTIN without one.
	if gstinNow && pan == "" {
		v.add("body.pan", "required with a GSTIN")
	}

	if sent(ctx, "upiId") {
		changed = append(changed, "upiId")
		params.UpiIDEnc, params.UpiIDLast4 = nil, nil
		if body.UpiId != nil {
			f, err := seal(FieldUPI, *body.UpiId, upiLast4(*body.UpiId))
			if err != nil {
				return params, nil, err
			}
			params.UpiIDEnc, params.UpiIDLast4 = f.enc, f.last4
		}
	}

	if sent(ctx, "bankAccount") {
		changed = append(changed, "bankAccount")
		params.BankAccountEnc, params.BankAccountLast4 = nil, nil
		if body.BankAccount != nil {
			f, err := seal(FieldBankAccount, *body.BankAccount, last4(*body.BankAccount))
			if err != nil {
				return params, nil, err
			}
			params.BankAccountEnc, params.BankAccountLast4 = f.enc, f.last4
		}
	}
	if sent(ctx, "bankIfsc") {
		changed = append(changed, "bankIfsc")
		params.BankIfsc = nil
		if body.BankIfsc != nil {
			params.BankIfsc = ptr(strings.ToUpper(*body.BankIfsc))
		}
	}
	if (params.BankAccountEnc != nil) != (params.BankIfsc != nil) {
		if params.BankIfsc == nil {
			v.add("body.bankIfsc", "required with a bank account")
		} else {
			v.add("body.bankAccount", "required with an IFSC")
		}
	}

	if sent(ctx, "legalName") {
		changed = append(changed, "legalName")
		v.text("body.legalName", body.LegalName)
		params.LegalName = body.LegalName
	}

	if err := v.err(); err != nil {
		return params, nil, err
	}
	params.Status = kycMissing
	if params.PanEnc != nil {
		params.Status = kycSubmitted
	}
	if changed == nil {
		changed = []string{}
	}
	return params, changed, nil
}
