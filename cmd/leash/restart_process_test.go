//go:build integration

// Process-level restart recovery.
//
// TestRestartRecovery in internal/engine proves the property at the database
// level: throw away every in-memory structure, reopen the file, and the
// sweeper still refunds against the original deadlines. It proves it inside
// one process, with a Go function call standing in for the crash.
//
// This test removes the stand-in. A real leash binary locks real escrows on
// Monad testnet, is killed with SIGKILL — no shutdown hook, no flush, no
// chance to finish a write — and a second binary is started on the same
// database file. Recovery is then checked in the two places that actually
// matter: the ledger's own rows, and the escrow contract.
//
// Run:
//
//	MONAD_RPC=https://testnet-rpc.monad.xyz BUYER_KEY=<hex> \
//	LEASH_DEFAULT_TTL=60 \
//	go test -v -tags integration -timeout 900s -run RestartProcess ./cmd/leash/...
package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/chain/bindings"
	"github.com/A1kartikey/leash/internal/ledger"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
)

// On-chain Status enum.
const (
	onchainLocked   = 0
	onchainRefunded = 2
)

const (
	tenant   = types.TenantID("agent-1")
	inFlight = 3 // obligations pending when the process dies
)

// ---------------------------------------------------------------------------
// Process plumbing
// ---------------------------------------------------------------------------

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func build(t *testing.T, pkg, out string) string {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = "../.." // package paths below are relative to the repo root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", pkg, err, b)
	}
	return out
}

// start launches a binary with its output captured to a file, dumped only if
// the test fails.
func start(t *testing.T, name, bin string, args ...string) *exec.Cmd {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), name+".log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}

	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
		f.Close()
		if t.Failed() {
			if b, err := os.ReadFile(logPath); err == nil {
				t.Logf("--- %s output ---\n%s", name, b)
			}
		}
	})
	return cmd
}

func waitReady(t *testing.T, url string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s never became ready", url)
}

// state is the subset of the dashboard snapshot this test reads.
type state struct {
	Locked    string `json:"locked"`
	Recovered string `json:"recovered"`
	Pending   int    `json:"pending"`
}

func snapshot(t *testing.T, base string) state {
	t.Helper()
	resp, err := http.Get(base + "/api/state")
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	defer resp.Body.Close()

	var s state
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decoding state: %v", err)
	}
	return s
}

// ---------------------------------------------------------------------------
// Chain reads, done by the test rather than by the process under test
// ---------------------------------------------------------------------------

func escrowStatus(t *testing.T, c *bindings.PaymentEscrow, id types.EscrowID) uint8 {
	t.Helper()
	e, err := c.Escrows(&bind.CallOpts{Context: context.Background()}, new(big.Int).SetUint64(uint64(id)))
	if err != nil {
		t.Fatalf("reading escrow %d: %v", id, err)
	}
	return e.Status
}

// obligations reads the ledger directly. Only ever called while no leash
// process is running, so the file has exactly one reader.
func obligations(t *testing.T, dbPath string) []types.Obligation {
	t.Helper()

	led, err := ledger.New(dbPath)
	if err != nil {
		t.Fatalf("opening ledger: %v", err)
	}
	defer led.Close()

	obs, err := led.Recent(context.Background(), tenant, 100)
	if err != nil {
		t.Fatalf("reading obligations: %v", err)
	}
	return obs
}

// ---------------------------------------------------------------------------

func TestRestartProcessRecoversPendingObligations(t *testing.T) {
	if os.Getenv("MONAD_RPC") == "" || os.Getenv("BUYER_KEY") == "" {
		t.Skip("MONAD_RPC and BUYER_KEY must be set: this test needs escrows that outlive the process")
	}

	cfg, err := chain.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ttl := time.Duration(cfg.DefaultTTL) * time.Second
	if ttl > 300*time.Second {
		t.Skipf("LEASH_DEFAULT_TTL=%ds makes this test longer than it is worth; set it to 60", cfg.DefaultTTL)
	}

	client, err := ethclient.Dial(cfg.RPC)
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}
	defer client.Close()
	contract, err := bindings.NewPaymentEscrow(cfg.ContractAddr, client)
	if err != nil {
		t.Fatalf("binding contract: %v", err)
	}

	bin := t.TempDir()
	leashBin := build(t, "./cmd/leash", filepath.Join(bin, "leash"))
	merchantBin := build(t, "./cmd/mockmerchant", filepath.Join(bin, "mockmerchant"))

	dbPath := filepath.Join(t.TempDir(), "leash.db")
	merchantPort := freePort(t)
	merchantURL := fmt.Sprintf("http://127.0.0.1:%d", merchantPort)

	// The merchant takes every lock and delivers nothing, so every obligation
	// is still LOCKED when the process dies — which is the state recovery has
	// to survive.
	start(t, "merchant", merchantBin,
		"-addr", fmt.Sprintf(":%d", merchantPort),
		"-mode", "fail",
		"-price", "100000000000000", // 0.0001 MON
	)
	waitReady(t, merchantURL+"/mode", 30*time.Second)

	leashArgs := func(port int) []string {
		return []string{
			"-addr", fmt.Sprintf(":%d", port),
			"-upstream", merchantURL,
			"-db", dbPath,
			"-ttl", ttl.String(),
			"-sweep-interval", "3s",
			"-sweep-grace", "2s",
			"-breaker-threshold", "99", // the breaker is not what is under test
		}
	}

	// --- process 1: lock, then die -----------------------------------------
	port1 := freePort(t)
	first := start(t, "leash-1", leashBin, leashArgs(port1)...)
	base1 := fmt.Sprintf("http://127.0.0.1:%d", port1)
	waitReady(t, base1+"/api/state", 60*time.Second)

	lockedAt := time.Now()
	for i := 0; i < inFlight; i++ {
		resp, err := http.Get(base1 + "/resource")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if v := resp.Header.Get("X-Leash-Verdict"); v != string(types.VerdictAbsent) {
			t.Fatalf("request %d: verdict %q, want absent", i, v)
		}
	}

	before := snapshot(t, base1)
	if before.Pending != inFlight {
		t.Fatalf("process 1 holds %d pending obligations, want %d", before.Pending, inFlight)
	}
	t.Logf("process 1 (pid %d): %d obligations pending, %s wei locked", first.Process.Pid, before.Pending, before.Locked)

	// SIGKILL. No deferred close, no flush, no sweeper tick on the way out.
	if time.Since(lockedAt) > ttl {
		t.Fatalf("locking took longer than the %s TTL; the obligations expired before the kill", ttl)
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL: %v", err)
	}
	first.Wait()
	t.Log("process 1 killed with SIGKILL")

	// --- the state the crash left behind ------------------------------------
	crashed := obligations(t, dbPath)
	if len(crashed) != inFlight {
		t.Fatalf("ledger holds %d obligations after the crash, want %d", len(crashed), inFlight)
	}

	deadlines := map[types.EscrowID]time.Time{}
	for _, ob := range crashed {
		if ob.Status != types.StatusLocked {
			t.Fatalf("escrow %d survived the crash as %s, want locked", ob.EscrowID, ob.Status)
		}
		if ob.SettleTx != "" {
			t.Fatalf("escrow %d claims settlement tx %s, but nothing settled it", ob.EscrowID, ob.SettleTx)
		}
		if got := escrowStatus(t, contract, ob.EscrowID); got != onchainLocked {
			t.Fatalf("escrow %d is %d on-chain after the crash, want Locked(%d)", ob.EscrowID, got, onchainLocked)
		}
		deadlines[ob.EscrowID] = ob.ReleaseDeadline
	}
	t.Logf("after the crash: %d escrows locked on-chain and unrecorded as settled", len(crashed))

	// --- process 2: same file, brand new everything -------------------------
	port2 := freePort(t)
	second := start(t, "leash-2", leashBin, leashArgs(port2)...)
	base2 := fmt.Sprintf("http://127.0.0.1:%d", port2)
	waitReady(t, base2+"/api/state", 60*time.Second)
	t.Log("process 2 started on the same database")

	// The restarted process needs no recovery code path: its sweeper simply
	// finds the rows on the next tick, once their original deadlines pass.
	deadline := time.Now().Add(ttl + 4*time.Minute)
	var after state
	for time.Now().Before(deadline) {
		after = snapshot(t, base2)
		if after.Pending == 0 && after.Recovered != "0" {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if after.Pending != 0 {
		t.Fatalf("process 2 still holds %d pending obligations; recovery did not happen", after.Pending)
	}

	wantRecovered := new(big.Int).Mul(big.NewInt(100_000_000_000_000), big.NewInt(inFlight))
	if after.Recovered != wantRecovered.String() {
		t.Fatalf("recovered %s wei, want %s", after.Recovered, wantRecovered)
	}
	t.Logf("process 2 recovered %s wei across %d obligations", after.Recovered, inFlight)

	// --- both sources of truth agree ----------------------------------------
	// Stop the writer first: the final reads are the test's own, and a single
	// reader on the file keeps them unambiguous.
	second.Process.Kill()
	second.Wait()

	for _, ob := range obligations(t, dbPath) {
		if ob.Status != types.StatusRefunded {
			t.Fatalf("escrow %d is %s in the ledger, want refunded", ob.EscrowID, ob.Status)
		}
		if ob.SettleTx == "" {
			t.Fatalf("escrow %d is refunded with no settlement tx recorded", ob.EscrowID)
		}
		if got := escrowStatus(t, contract, ob.EscrowID); got != onchainRefunded {
			t.Fatalf("escrow %d is %d on-chain, want Refunded(%d)", ob.EscrowID, got, onchainRefunded)
		}
		// The deadline is the one the dead process wrote. A restart that reset
		// it would refund late — or, with a longer TTL, not at all.
		if was := deadlines[ob.EscrowID]; !ob.ReleaseDeadline.Equal(was) {
			t.Fatalf("escrow %d came back with deadline %s, but the crash left %s",
				ob.EscrowID, ob.ReleaseDeadline, was)
		}
	}
	t.Log("ledger and chain agree: every obligation refunded against its original deadline")
}
