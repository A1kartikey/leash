package engine

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// 402 challenge headers. The merchant sets these on the 402; the sidecar
// echoes the escrow headers on the retry.
const (
	HdrPrice        = "X-Payment-Price"         // wei, decimal
	HdrMerchant     = "X-Payment-Merchant"      // 0x address
	HdrResource     = "X-Payment-Resource"      // opaque resource id
	HdrResourceHash = "X-Payment-Resource-Hash" // 0x + 32-byte keccak of the body
	HdrContentType  = "X-Payment-Content-Type"  // declared content contract
	HdrMinBytes     = "X-Payment-Min-Bytes"     // declared content contract
	HdrEscrowID     = "X-Payment-Escrow-Id"     // set on the retry
	HdrLockTx       = "X-Payment-Tx"            // set on the retry
)

// ParseChallenge reads a 402 response's headers into a Challenge.
//
// The merchant is untrusted input: everything here is validated before a
// single wei is locked. maxPrice caps the price a merchant may demand; nil or
// zero disables the cap.
func ParseChallenge(h http.Header, maxPrice *big.Int) (types.Challenge, error) {
	var c types.Challenge

	price, ok := new(big.Int).SetString(strings.TrimSpace(h.Get(HdrPrice)), 10)
	if !ok || price.Sign() <= 0 {
		return c, fmt.Errorf("challenge: bad %s %q", HdrPrice, h.Get(HdrPrice))
	}
	if maxPrice != nil && maxPrice.Sign() > 0 && price.Cmp(maxPrice) > 0 {
		return c, fmt.Errorf("challenge: price %s wei exceeds cap %s wei", price, maxPrice)
	}

	m := strings.TrimSpace(h.Get(HdrMerchant))
	if !common.IsHexAddress(m) {
		return c, fmt.Errorf("challenge: bad %s %q", HdrMerchant, m)
	}
	merchant := common.HexToAddress(m)
	if merchant == (common.Address{}) {
		return c, fmt.Errorf("challenge: zero merchant address")
	}

	c.Price = price
	c.Merchant = merchant
	c.ResourceID = h.Get(HdrResource)
	c.ContentType = strings.TrimSpace(h.Get(HdrContentType))

	if raw := strings.TrimSpace(h.Get(HdrResourceHash)); raw != "" {
		b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
		if err != nil || len(b) != 32 {
			return types.Challenge{}, fmt.Errorf("challenge: bad %s %q", HdrResourceHash, raw)
		}
		copy(c.ResourceHash[:], b)
	}

	if raw := strings.TrimSpace(h.Get(HdrMinBytes)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return types.Challenge{}, fmt.Errorf("challenge: bad %s %q", HdrMinBytes, raw)
		}
		c.MinBytes = n
	}

	return c, nil
}

// WriteChallenge sets the 402 headers a merchant must send. Shared with the
// SDK middleware and the mock merchant so both speak the same dialect.
func WriteChallenge(h http.Header, c types.Challenge) {
	h.Set(HdrPrice, c.Price.String())
	h.Set(HdrMerchant, c.Merchant.Hex())
	if c.ResourceID != "" {
		h.Set(HdrResource, c.ResourceID)
	}
	if c.ResourceHash != zeroHash {
		h.Set(HdrResourceHash, "0x"+hex.EncodeToString(c.ResourceHash[:]))
	}
	if c.ContentType != "" {
		h.Set(HdrContentType, c.ContentType)
	}
	if c.MinBytes > 0 {
		h.Set(HdrMinBytes, strconv.Itoa(c.MinBytes))
	}
}
