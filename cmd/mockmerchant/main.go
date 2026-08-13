// Command mockmerchant is a throwaway demo merchant that speaks the Leash 402
// dialect and can misbehave on demand.
//
// Modes are switchable mid-run without a restart:
//
//	honest                  200 + the exact advertised body   -> Delivered
//	fail-after-settlement   200 + nothing at all              -> Absent
//	partial                 200 + a truncated body            -> Partial
//
//	curl localhost:8081/resource                 # 402 challenge
//	curl -X POST 'localhost:8081/mode?m=partial' # switch, no restart
package main

import (
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync/atomic"

	"github.com/A1kartikey/leash/internal/engine"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// The advertised body is fixed so its keccak256 can be published in the 402.
const canonicalBody = `{"resource":"weather/nyc","temp_c":21,"wind_kph":11,"observed":"2026-01-01T00:00:00Z"}`

const (
	modeHonest  = "honest"
	modePartial = "partial"
	modeFail    = "fail-after-settlement"
)

var mode atomic.Value // string

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	merchant := flag.String("merchant", "0x000000000000000000000000000000000000dEaD", "merchant payout address")
	priceWei := flag.String("price", "1000000000000000", "price in wei (0.001 MON)")
	start := flag.String("mode", modeHonest, "starting mode: honest | partial | fail-after-settlement")
	flag.Parse()

	if !common.IsHexAddress(*merchant) {
		log.Fatalf("mockmerchant: %q is not an address", *merchant)
	}
	price, ok := new(big.Int).SetString(*priceWei, 10)
	if !ok || price.Sign() <= 0 {
		log.Fatalf("mockmerchant: bad price %q", *priceWei)
	}
	if !validMode(*start) {
		log.Fatalf("mockmerchant: unknown mode %q", *start)
	}
	mode.Store(*start)

	challenge := types.Challenge{
		Price:        price,
		Merchant:     common.HexToAddress(*merchant),
		ResourceID:   "weather/nyc",
		ResourceHash: crypto.Keccak256Hash([]byte(canonicalBody)),
		ContentType:  "application/json",
		MinBytes:     16, // low enough that a truncated body still conforms
	}

	http.HandleFunc("/mode", modeHandler)
	http.HandleFunc("/resource", func(w http.ResponseWriter, r *http.Request) {
		resource(w, r, challenge)
	})

	log.Printf("mockmerchant on %s: mode=%s price=%s wei merchant=%s", *addr, *start, price, challenge.Merchant.Hex())
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func resource(w http.ResponseWriter, r *http.Request, c types.Challenge) {
	if r.Header.Get(engine.HdrEscrowID) == "" {
		engine.WriteChallenge(w.Header(), c)
		w.WriteHeader(http.StatusPaymentRequired)
		return
	}

	m := mode.Load().(string)
	log.Printf("serving escrow %s in mode %s", r.Header.Get(engine.HdrEscrowID), m)

	w.Header().Set("Content-Type", "application/json")
	switch m {
	case modeFail:
		// Took the lock, returns nothing. A naive client settles anyway.
		w.WriteHeader(http.StatusOK)
	case modePartial:
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, canonicalBody[:len(canonicalBody)/2])
	default:
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, canonicalBody)
	}
}

func modeHandler(w http.ResponseWriter, r *http.Request) {
	if m := r.URL.Query().Get("m"); m != "" {
		if !validMode(m) {
			http.Error(w, "unknown mode "+m, http.StatusBadRequest)
			return
		}
		mode.Store(m)
		log.Printf("mode -> %s", m)
	}
	fmt.Fprintln(w, mode.Load().(string))
}

func validMode(m string) bool {
	return m == modeHonest || m == modePartial || m == modeFail
}
