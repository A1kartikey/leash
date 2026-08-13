// Package chain implements the types.Chain interface by wrapping the
// PaymentEscrow contract via go-ethereum bindings.
package chain

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Config holds everything the EscrowChain wrapper needs to connect to the
// contract and sign transactions. Keys come from env only — never a literal,
// never committed.
type Config struct {
	RPC          string
	PrivateKey   *ecdsa.PrivateKey
	SignerAddr   common.Address
	ContractAddr common.Address
	ChainID      *big.Int
	DefaultTTL   uint64 // seconds; used when callers don't specify
}

// deploymentsFile mirrors the JSON in deployments.json.
type deploymentsFile struct {
	MonadTestnet struct {
		ChainID       int    `json:"chain_id"`
		EscrowAddress string `json:"escrow_address"`
		DeployedBlock uint64 `json:"deployed_at_block"`
	} `json:"monad_testnet"`
}

// LoadConfig reads deployment address from deployments.json and keys from the
// environment. It returns a fully populated Config or an error describing the
// first missing piece.
//
// Environment variables:
//
//	MONAD_RPC   — JSON-RPC endpoint (required)
//	BUYER_KEY   — hex-encoded ECDSA private key, no 0x prefix (required)
//	LEASH_DEPLOYMENTS — optional override for deployments.json path
//	LEASH_DEFAULT_TTL — optional override for default TTL (default: 3600)
func LoadConfig() (Config, error) {
	rpc := os.Getenv("MONAD_RPC")
	if rpc == "" {
		return Config{}, fmt.Errorf("MONAD_RPC not set")
	}

	keyHex := os.Getenv("BUYER_KEY")
	if keyHex == "" {
		return Config{}, fmt.Errorf("BUYER_KEY not set")
	}
	keyHex = strings.TrimPrefix(keyHex, "0x")

	pk, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return Config{}, fmt.Errorf("parsing BUYER_KEY: %w", err)
	}

	addr := crypto.PubkeyToAddress(pk.PublicKey)

	depPath := os.Getenv("LEASH_DEPLOYMENTS")
	if depPath == "" {
		// Walk up from the binary / cwd to find deployments.json at repo root.
		depPath = findDeployments()
	}

	raw, err := os.ReadFile(depPath)
	if err != nil {
		return Config{}, fmt.Errorf("reading deployments.json at %s: %w", depPath, err)
	}

	var dep deploymentsFile
	if err := json.Unmarshal(raw, &dep); err != nil {
		return Config{}, fmt.Errorf("parsing deployments.json: %w", err)
	}

	if dep.MonadTestnet.EscrowAddress == "" || dep.MonadTestnet.EscrowAddress == "0x0000000000000000000000000000000000000000" {
		return Config{}, fmt.Errorf("escrow_address in deployments.json is not set — deploy the contract first")
	}

	ttl := uint64(3600)
	if v := os.Getenv("LEASH_DEFAULT_TTL"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &ttl); err != nil {
			return Config{}, fmt.Errorf("parsing LEASH_DEFAULT_TTL: %w", err)
		}
	}

	return Config{
		RPC:          rpc,
		PrivateKey:   pk,
		SignerAddr:   addr,
		ContractAddr: common.HexToAddress(dep.MonadTestnet.EscrowAddress),
		ChainID:      big.NewInt(int64(dep.MonadTestnet.ChainID)),
		DefaultTTL:   ttl,
	}, nil
}

// findDeployments walks upward from cwd looking for deployments.json.
func findDeployments() string {
	dir, _ := os.Getwd()
	for {
		p := filepath.Join(dir, "deployments.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "deployments.json" // fallback — will produce a clear error
}
