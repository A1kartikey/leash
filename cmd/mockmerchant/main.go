// Command mockmerchant is a throwaway demo merchant that speaks the Leash 402
// dialect and can misbehave on demand.
//
// It has no 402 handling of its own: the challenge and the escrow check both
// come from sdk/leash, so running the demo rig is the SDK's integration test.
// The handler below only decides what to serve once the money is confirmed
// locked — which is exactly the division of labour the SDK is meant to give a
// real merchant.
//
// Modes switch at runtime, without a restart — a restart mid-demo is the
// commonest way a live demo visibly breaks:
//
//	honest    200 + the exact advertised body       -> Delivered
//	fail      502 + no body, after taking the lock  -> Absent
//	partial   200 + a truncated body                -> Partial
//
//	curl localhost:8081/resource                 # 402 challenge
//	curl -X POST localhost:8081/mode -d partial  # switch, no restart
//	curl -X POST 'localhost:8081/mode?m=honest'  # same thing, query form
//	curl localhost:8081/mode                     # what mode am I in?
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/A1kartikey/leash/internal/engine"
	"github.com/A1kartikey/leash/sdk/leash"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// The advertised body is fixed so its keccak256 can be published in the 402.
const canonicalBody = `{"resource":"weather/nyc","temp_c":21,"wind_kph":11,"observed":"2026-01-01T00:00:00Z"}`

const (
	modeHonest  = "honest"
	modePartial = "partial"
	modeFail    = "fail"
)

// aliases keeps older scripts and muscle memory working.
var aliases = map[string]string{
	modeHonest:              modeHonest,
	modePartial:             modePartial,
	modeFail:                modeFail,
	"fail-after-settlement": modeFail,
	"absent":                modeFail,
	"deliver":               modeHonest,
}

var mode atomic.Value // string

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	merchant := flag.String("merchant", "0x000000000000000000000000000000000000dEaD", "merchant payout address")
	priceWei := flag.String("price", "1000000000000000", "price in wei (0.001 MON)")
	start := flag.String("mode", modeHonest, "starting mode: honest | fail | partial")
	rpc := flag.String("rpc", "", "JSON-RPC endpoint used to confirm escrows (default $MONAD_RPC)")
	escrow := flag.String("escrow", "", "PaymentEscrow address (default $LEASH_ESCROW)")
	flag.Parse()

	if !common.IsHexAddress(*merchant) {
		log.Fatalf("mockmerchant: %q is not an address", *merchant)
	}
	price, ok := new(big.Int).SetString(*priceWei, 10)
	if !ok || price.Sign() <= 0 {
		log.Fatalf("mockmerchant: bad price %q", *priceWei)
	}
	m, ok := aliases[strings.ToLower(strings.TrimSpace(*start))]
	if !ok {
		log.Fatalf("mockmerchant: unknown mode %q", *start)
	}
	mode.Store(m)

	merchantAddr := common.HexToAddress(*merchant)
	terms := leash.Terms{
		Price:        price,
		Merchant:     merchantAddr,
		ResourceID:   "weather/nyc",
		ResourceHash: crypto.Keccak256Hash([]byte(canonicalBody)),
		ContentType:  "application/json",
		MinBytes:     16, // low enough that a truncated body still conforms
	}

	log.SetFlags(log.Ltime)
	connect(*rpc, *escrow, terms)

	http.HandleFunc("/mode", modeHandler)
	http.Handle("/resource", commentary(terms, leash.Paid(terms, http.HandlerFunc(serve))))

	log.Printf("mockmerchant on %s: mode=%s price=%s wei merchant=%s", *addr, m, price, merchantAddr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// connect points the SDK at the escrow contract. With no RPC configured the
// merchant cannot confirm anything, so it falls back to trusting the escrow
// header — fine for the offline demo, loudly wrong anywhere else.
func connect(rpc, escrow string, t leash.Terms) {
	if rpc == "" {
		rpc = strings.TrimSpace(os.Getenv("MONAD_RPC"))
	}
	if escrow == "" {
		escrow = strings.TrimSpace(os.Getenv("LEASH_ESCROW"))
	}

	if rpc == "" || !common.IsHexAddress(escrow) {
		leash.UseReader(leash.AllowAll(t))
		log.Print("ESCROW CHECK DISABLED — no -rpc/-escrow, serving on trust (offline demo only)")
		return
	}

	if err := leash.Connect(context.Background(), rpc, common.HexToAddress(escrow)); err != nil {
		log.Fatalf("mockmerchant: %v", err)
	}
	log.Printf("escrow check live: %s via %s", escrow, rpc)
}

// commentary logs the 402s the SDK issues, which the merchant never sees
// otherwise — the middleware short-circuits before the handler. Presenter's
// commentary track, not something a real merchant needs.
func commentary(t leash.Terms, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == http.StatusPaymentRequired {
			log.Printf("402  %-12s  price %s wei — %s", t.ResourceID, t.Price, rec.Header().Get("X-Payment-Reason"))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// serve runs only after sdk/leash has confirmed a locked escrow for this
// request. What it returns is the demo's misbehaviour, not the SDK's.
func serve(w http.ResponseWriter, r *http.Request) {
	m := mode.Load().(string)
	escrow := r.Header.Get(engine.HdrEscrowID)

	// One line per paid request: mode in, outcome out. If the UI ever lags,
	// this is the commentary track.
	switch m {
	case modeFail:
		// Took the lock, then refuses to deliver. A naive client settles anyway.
		w.WriteHeader(http.StatusBadGateway)
		log.Printf("PAID escrow=%-4s mode=%-7s -> 502, 0 bytes            (absent: nothing delivered)", escrow, m)

	case modePartial:
		body := canonicalBody[:len(canonicalBody)/2]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, body)
		log.Printf("PAID escrow=%-4s mode=%-7s -> 200, %d/%d bytes         (partial: shape ok, wrong hash)",
			escrow, m, len(body), len(canonicalBody))

	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, canonicalBody)
		log.Printf("PAID escrow=%-4s mode=%-7s -> 200, %d bytes           (delivered: hash matches)",
			escrow, m, len(canonicalBody))
	}
}

// modeHandler switches modes at runtime. The mode may arrive as a query
// parameter, a form field, a bare body, or JSON — whatever the presenter's
// fingers reach for under stage lights.
func modeHandler(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("m")
	if raw == "" {
		raw = r.URL.Query().Get("mode")
	}
	if raw == "" && r.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<12))
		raw = parseModeBody(b)
	}

	if raw != "" {
		m, ok := aliases[strings.ToLower(strings.TrimSpace(raw))]
		if !ok {
			http.Error(w, "unknown mode "+raw, http.StatusBadRequest)
			return
		}
		mode.Store(m)
		log.Printf("MODE -> %s", strings.ToUpper(m))
	}
	fmt.Fprintln(w, mode.Load().(string))
}

// parseModeBody accepts {"mode":"fail"}, mode=fail, or just fail.
func parseModeBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "{") {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(s), &body); err == nil {
			return body.Mode
		}
		return ""
	}
	if k, v, ok := strings.Cut(s, "="); ok && (k == "m" || k == "mode") {
		return v
	}
	return s
}
