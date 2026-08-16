package engine

import (
	"context"
	"mime"
	"strings"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Reason codes returned alongside a Verdict. Stable strings — the UI and the
// ledger both key off them.
const (
	ReasonHashMatch        = "hash_match"
	ReasonContentContract  = "content_contract"
	ReasonHashMismatch     = "hash_mismatch"
	ReasonBadStatus        = "bad_status"
	ReasonEmptyBody        = "empty_body"
	ReasonContractViolated = "content_contract_violated"
)

var zeroHash [32]byte

// Judge is the settlement decision. It is a pure function: no I/O, no model
// call, no heuristic, no scoring. Every input is an argument and the same
// inputs always produce the same verdict.
//
//	2xx AND len(body) > 0 AND (keccak256(body) == resourceHash
//	                           OR satisfies the declared content contract)
//
// Partial exists for exactly one case: a response that conforms to the
// declared content contract but is not the resource that was paid for.
//
// Any pressure to make this "smarter" belongs outside the settlement path.
func Judge(c types.Challenge, status int, contentType string, body []byte) types.Verdict {
	if status < 200 || status > 299 {
		return types.Verdict{Outcome: types.VerdictAbsent, Reason: ReasonBadStatus}
	}
	if len(body) == 0 {
		return types.Verdict{Outcome: types.VerdictAbsent, Reason: ReasonEmptyBody}
	}

	contract := satisfiesContract(c, contentType, body)

	if c.ResourceHash == zeroHash {
		// No hash declared: the content contract is the whole agreement.
		if contract {
			return types.Verdict{Outcome: types.VerdictDelivered, Reason: ReasonContentContract}
		}
		return types.Verdict{Outcome: types.VerdictAbsent, Reason: ReasonContractViolated}
	}

	if crypto.Keccak256Hash(body) == c.ResourceHash {
		return types.Verdict{Outcome: types.VerdictDelivered, Reason: ReasonHashMatch}
	}
	// Partial requires an explicit content contract to fall back on: something
	// of the declared shape arrived, but not the paid-for resource. With no
	// contract declared there is nothing to conform to, so a mismatch is absent.
	if contract && (c.ContentType != "" || c.MinBytes > 0) {
		return types.Verdict{Outcome: types.VerdictPartial, Reason: ReasonHashMismatch}
	}
	return types.Verdict{Outcome: types.VerdictAbsent, Reason: ReasonHashMismatch}
}

// satisfiesContract reports whether the response meets the declared shape.
// An undeclared field is not a constraint.
func satisfiesContract(c types.Challenge, contentType string, body []byte) bool {
	if len(body) < c.MinBytes {
		return false
	}
	if c.ContentType != "" {
		got, _, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.EqualFold(got, c.ContentType) {
			return false
		}
	}
	return true
}

// Verifier adapts Judge to types.Verifier. Like Judge it is pure: the caller
// reads and closes the response body and hands the bytes over, so the response
// is still readable downstream (logging, forwarding to the agent).
type Verifier struct{}

var _ types.Verifier = Verifier{}

func (Verifier) Verify(_ context.Context, c types.Challenge, status int, contentType string, body []byte) (types.Verdict, error) {
	return Judge(c, status, contentType, body), nil
}
