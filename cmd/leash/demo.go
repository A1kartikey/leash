package main

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/A1kartikey/leash/internal/types/mocks"
	"github.com/ethereum/go-ethereum/common"
)

// demoChain is an in-memory stand-in for the escrow contract so the dashboard
// runs with no funds and no RPC. It moves balances for real — that is the
// whole point of the screen: merchant revenue must stay flat while the
// buyer's Locked figure climbs.
type demoChain struct {
	*mocks.MockChain

	buyer common.Address

	mu       sync.Mutex
	nextID   uint64
	balances map[common.Address]*big.Int
	escrows  map[types.EscrowID]*demoEscrow
}

type demoEscrow struct {
	merchant common.Address
	amount   *big.Int
	settled  bool
}

func newDemoChain() *demoChain {
	buyer := common.HexToAddress("0x00000000000000000000000000000000000ABC0D")
	d := &demoChain{
		MockChain: &mocks.MockChain{},
		buyer:     buyer,
		balances:  map[common.Address]*big.Int{buyer: mon(10)},
		escrows:   make(map[types.EscrowID]*demoEscrow),
	}

	d.LockFn = d.lock
	d.ReleaseFn = func(_ context.Context, id types.EscrowID) (types.TxHash, error) {
		return d.settle(id, nil)
	}
	d.RefundFn = func(_ context.Context, id types.EscrowID) (types.TxHash, error) {
		return d.settle(id, big.NewInt(0))
	}
	d.ReleasePartialFn = func(_ context.Context, id types.EscrowID, amount *big.Int) (types.TxHash, error) {
		return d.settle(id, amount)
	}
	d.BalanceOfFn = d.balanceOf
	return d
}

func mon(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

func (d *demoChain) lock(_ context.Context, merchant common.Address, amount *big.Int, _ [32]byte) (types.EscrowID, types.TxHash, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.balance(d.buyer).Cmp(amount) < 0 {
		return 0, "", fmt.Errorf("demo chain: buyer balance below %s wei", amount)
	}
	d.balance(d.buyer).Sub(d.balance(d.buyer), amount)

	d.nextID++
	id := types.EscrowID(d.nextID)
	d.escrows[id] = &demoEscrow{merchant: merchant, amount: new(big.Int).Set(amount)}
	return id, types.TxHash(fmt.Sprintf("0xdemo%064x", d.nextID)), nil
}

// settle pays toMerchant wei to the merchant and returns the rest to the
// buyer. A nil share means the full amount (release); zero means none (refund).
func (d *demoChain) settle(id types.EscrowID, toMerchant *big.Int) (types.TxHash, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	e, ok := d.escrows[id]
	if !ok || e.settled {
		return "", fmt.Errorf("demo chain: escrow %d is not locked", id)
	}
	if toMerchant == nil {
		toMerchant = e.amount
	}
	if toMerchant.Cmp(e.amount) > 0 {
		return "", fmt.Errorf("demo chain: %s wei exceeds escrow %d", toMerchant, id)
	}

	e.settled = true
	d.balance(e.merchant).Add(d.balance(e.merchant), toMerchant)
	d.balance(d.buyer).Add(d.balance(d.buyer), new(big.Int).Sub(e.amount, toMerchant))
	return types.TxHash(fmt.Sprintf("0xdemosettle%053x", uint64(id))), nil
}

func (d *demoChain) balanceOf(_ context.Context, addr common.Address) (*big.Int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return new(big.Int).Set(d.balance(addr)), nil
}

// balance returns the mutable balance for addr. Callers hold d.mu.
func (d *demoChain) balance(addr common.Address) *big.Int {
	b, ok := d.balances[addr]
	if !ok {
		b = new(big.Int)
		d.balances[addr] = b
	}
	return b
}
