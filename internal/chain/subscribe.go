package chain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/A1kartikey/leash/internal/chain/bindings"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ---------------------------------------------------------------------------
// Event discovery by filter
//
// Escrow IDs come from a counter shared with every other user of the
// singleton — strangers take IDs between yours. We subscribe to
// EscrowLocked filtered on the buyer address. Never iterate an ID range,
// never assume contiguity, never infer "my next ID."
//
// Strategy: poll-based filter. Monad testnet may not support eth_subscribe
// over HTTP. We poll FilterEscrowLocked (and Released/Refunded/Partial)
// every pollInterval from a cursor block.
//
// EscrowLocked and EscrowRefunded index the buyer, so the node filters those
// for us. EscrowReleased indexes (id, merchant) — the buyer is not in its
// topics, so no log filter can scope it and we must attribute it ourselves
// from the escrow IDs we have seen locked. Only a buyer can release their own
// escrow, so a release we cannot attribute is not ours to report.
// ---------------------------------------------------------------------------

const pollInterval = 2 * time.Second

// eventPoller streams escrow events for a buyer address by polling
// FilterLogs. It writes to the provided channel and closes it when ctx
// is cancelled.
func eventPoller(
	ctx context.Context,
	client *ethclient.Client,
	contract *bindings.PaymentEscrow,
	buyer common.Address,
	startBlock uint64,
	out chan<- types.EscrowEvent,
) {
	defer close(out)

	cursor := startBlock
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Escrow IDs this poller has seen locked by our buyer. It is the only way
	// to tell our releases from a stranger's, and it stays bounded: an id is
	// dropped the moment the escrow settles.
	mine := make(map[uint64]bool)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			head, err := client.BlockNumber(ctx)
			if err != nil || head < cursor {
				continue
			}

			end := head
			opts := &bind.FilterOpts{
				Start:   cursor,
				End:     &end,
				Context: ctx,
			}

			// --- EscrowLocked (filtered on buyer) ---
			lockedIter, err := contract.FilterEscrowLocked(opts, nil, []common.Address{buyer}, nil)
			if err == nil {
				for lockedIter.Next() {
					ev := lockedIter.Event
					mine[ev.Id.Uint64()] = true
					select {
					case out <- types.EscrowEvent{
						Kind:        types.EventLocked,
						EscrowID:    types.EscrowID(ev.Id.Uint64()),
						TxHash:      types.TxHash(ev.Raw.TxHash.Hex()),
						BlockNumber: ev.Raw.BlockNumber,
					}:
					case <-ctx.Done():
						lockedIter.Close()
						return
					}
				}
				lockedIter.Close()
			}

			// --- EscrowReleased ---
			releasedIter, err := contract.FilterEscrowReleased(opts, nil, nil)
			if err == nil {
				for releasedIter.Next() {
					ev := releasedIter.Event
					// Indexed by merchant, so this iterator carries every
					// release on the singleton. Ours are the ones we watched
					// get locked; the rest belong to other buyers entirely.
					if !mine[ev.Id.Uint64()] {
						continue
					}
					delete(mine, ev.Id.Uint64())
					select {
					case out <- types.EscrowEvent{
						Kind:        types.EventReleased,
						EscrowID:    types.EscrowID(ev.Id.Uint64()),
						TxHash:      types.TxHash(ev.Raw.TxHash.Hex()),
						BlockNumber: ev.Raw.BlockNumber,
					}:
					case <-ctx.Done():
						releasedIter.Close()
						return
					}
				}
				releasedIter.Close()
			}

			// --- EscrowRefunded (filtered on buyer) ---
			refundedIter, err := contract.FilterEscrowRefunded(opts, nil, []common.Address{buyer})
			if err == nil {
				for refundedIter.Next() {
					ev := refundedIter.Event
					delete(mine, ev.Id.Uint64())
					select {
					case out <- types.EscrowEvent{
						Kind:        types.EventRefunded,
						EscrowID:    types.EscrowID(ev.Id.Uint64()),
						TxHash:      types.TxHash(ev.Raw.TxHash.Hex()),
						BlockNumber: ev.Raw.BlockNumber,
					}:
					case <-ctx.Done():
						refundedIter.Close()
						return
					}
				}
				refundedIter.Close()
			}

			// Advance cursor past the scanned range.
			cursor = end + 1
		}
	}
}

// reconcileFromBlock replays historical events from startBlock to latest and
// pushes them into the channel. Used on startup to catch events missed during
// downtime.
func reconcileFromBlock(
	ctx context.Context,
	contract *bindings.PaymentEscrow,
	buyer common.Address,
	startBlock uint64,
) ([]types.EscrowEvent, uint64, error) {
	opts := &bind.FilterOpts{
		Start:   startBlock,
		Context: ctx,
	}

	var events []types.EscrowEvent

	// Locked events for our buyer.
	lockedIter, err := contract.FilterEscrowLocked(opts, nil, []common.Address{buyer}, nil)
	if err != nil {
		return nil, startBlock, fmt.Errorf("reconcile locked: %w", err)
	}
	defer lockedIter.Close()

	var maxBlock uint64
	mine := make(map[uint64]bool)
	for lockedIter.Next() {
		ev := lockedIter.Event
		mine[ev.Id.Uint64()] = true
		events = append(events, types.EscrowEvent{
			Kind:        types.EventLocked,
			EscrowID:    types.EscrowID(ev.Id.Uint64()),
			TxHash:      types.TxHash(ev.Raw.TxHash.Hex()),
			BlockNumber: ev.Raw.BlockNumber,
		})
		if ev.Raw.BlockNumber > maxBlock {
			maxBlock = ev.Raw.BlockNumber
		}
	}

	// Refunded events for our buyer.
	refundedIter, err := contract.FilterEscrowRefunded(opts, nil, []common.Address{buyer})
	if err != nil {
		return events, maxBlock, fmt.Errorf("reconcile refunded: %w", err)
	}
	defer refundedIter.Close()

	for refundedIter.Next() {
		ev := refundedIter.Event
		events = append(events, types.EscrowEvent{
			Kind:        types.EventRefunded,
			EscrowID:    types.EscrowID(ev.Id.Uint64()),
			TxHash:      types.TxHash(ev.Raw.TxHash.Hex()),
			BlockNumber: ev.Raw.BlockNumber,
		})
		if ev.Raw.BlockNumber > maxBlock {
			maxBlock = ev.Raw.BlockNumber
		}
	}

	// Released events are indexed by merchant, so this pass sees every buyer's
	// releases. Ours are the ones whose locks the pass above just replayed —
	// a release outside that set belongs to another buyer, and reporting it
	// would put a stranger's escrow id into our recovery path.
	releasedIter, err := contract.FilterEscrowReleased(opts, nil, nil)
	if err != nil {
		return events, maxBlock, fmt.Errorf("reconcile released: %w", err)
	}
	defer releasedIter.Close()

	for releasedIter.Next() {
		ev := releasedIter.Event
		if !mine[ev.Id.Uint64()] {
			continue
		}
		events = append(events, types.EscrowEvent{
			Kind:        types.EventReleased,
			EscrowID:    types.EscrowID(ev.Id.Uint64()),
			TxHash:      types.TxHash(ev.Raw.TxHash.Hex()),
			BlockNumber: ev.Raw.BlockNumber,
		})
		if ev.Raw.BlockNumber > maxBlock {
			maxBlock = ev.Raw.BlockNumber
		}
	}

	// Suppressing the unused variable — startBlock is the input, maxBlock is
	// the furthest block we've seen.
	_ = startBlock

	return events, maxBlock, nil
}

// escrowIDFromLockReceipt finds the EscrowLocked event in a tx receipt and
// extracts the escrow ID. This is the only reliable way to learn the ID —
// never infer it from nextId or assume contiguity.
func escrowIDFromLockReceipt(contract *bindings.PaymentEscrow, receipt receiptLike) (*big.Int, error) {
	for _, log := range receipt.GetLogs() {
		ev, err := contract.ParseEscrowLocked(log)
		if err != nil {
			continue
		}
		return ev.Id, nil
	}
	return nil, fmt.Errorf("EscrowLocked event not found in receipt")
}

// escrowIDsFromBatchReceipt extracts all EscrowLocked IDs from a lockBatch
// receipt, preserving order.
func escrowIDsFromBatchReceipt(contract *bindings.PaymentEscrow, receipt receiptLike) ([]*big.Int, error) {
	var ids []*big.Int
	for _, log := range receipt.GetLogs() {
		ev, err := contract.ParseEscrowLocked(log)
		if err != nil {
			continue
		}
		ids = append(ids, ev.Id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no EscrowLocked events found in receipt")
	}
	return ids, nil
}
