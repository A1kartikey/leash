// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {PaymentEscrow} from "../src/PaymentEscrow.sol";

/// @dev Re-enters the escrow from `receive()`. Acts as buyer and merchant of the
///      same escrow so that every access check passes and the ReentrancyGuard is
///      demonstrably the only thing standing between an attacker and the pool.
contract Reenterer {
    enum Mode {
        None,
        Release,
        Refund,
        ReleasePartial,
        Claim,
        ReleaseMany,
        RefundMany
    }

    PaymentEscrow public immutable escrow;
    Mode public mode;
    uint256 public targetId;
    uint256 public reentries;
    bytes public lastError;

    constructor(PaymentEscrow _escrow) {
        escrow = _escrow;
    }

    function arm(Mode _mode, uint256 _targetId) external {
        mode = _mode;
        targetId = _targetId;
    }

    /// @dev Spends the attacker's own balance so the accounting in the test is
    ///      unambiguous: whatever it ends with came out of the escrow.
    function lock(address merchant, bytes32 hash, uint64 ttl, uint256 amount)
        external
        returns (uint256)
    {
        return escrow.lock{value: amount}(merchant, hash, ttl);
    }

    function callRelease(uint256 id) external {
        escrow.release(id);
    }

    function callRefund(uint256 id) external {
        escrow.refund(id);
    }

    function callReleasePartial(uint256 id, uint256 amount) external {
        escrow.releasePartial(id, amount);
    }

    function callClaim(uint256 id) external {
        escrow.claim(id);
    }

    function callReleaseMany(uint256[] calldata ids) external {
        escrow.releaseMany(ids);
    }

    function callRefundMany(uint256[] calldata ids) external {
        escrow.refundMany(ids);
    }

    receive() external payable {
        if (mode == Mode.None) return;

        uint256[] memory ids = new uint256[](1);
        ids[0] = targetId;

        bytes memory data;
        if (mode == Mode.Release) {
            data = abi.encodeCall(PaymentEscrow.release, (targetId));
        } else if (mode == Mode.Refund) {
            data = abi.encodeCall(PaymentEscrow.refund, (targetId));
        } else if (mode == Mode.ReleasePartial) {
            data = abi.encodeCall(PaymentEscrow.releasePartial, (targetId, 1));
        } else if (mode == Mode.Claim) {
            data = abi.encodeCall(PaymentEscrow.claim, (targetId));
        } else if (mode == Mode.ReleaseMany) {
            data = abi.encodeCall(PaymentEscrow.releaseMany, (ids));
        } else {
            data = abi.encodeCall(PaymentEscrow.refundMany, (ids));
        }

        reentries++;
        (bool ok, bytes memory err) = address(escrow).call(data);
        // If this ever succeeds the pool is drainable. Fail loudly rather than
        // letting the outer transaction report success.
        require(!ok, "REENTRY SUCCEEDED");
        lastError = err;
    }
}

/// @dev Rejects every incoming transfer.
contract Rejector {
    receive() external payable {
        revert("no");
    }

    function callRelease(PaymentEscrow escrow, uint256 id) external {
        escrow.release(id);
    }

    function lock(PaymentEscrow escrow, address merchant, uint64 ttl) external payable returns (uint256) {
        return escrow.lock{value: msg.value}(merchant, bytes32(0), ttl);
    }
}

contract PaymentEscrowTest is Test {
    PaymentEscrow escrow;

    address buyer = makeAddr("buyer");
    address merchant = makeAddr("merchant");
    address stranger = makeAddr("stranger");

    bytes32 constant HASH = keccak256("GET /v1/quote");
    uint64 constant TTL = 1 hours;
    uint256 constant AMOUNT = 1 ether;

    function setUp() public {
        escrow = new PaymentEscrow();
        vm.warp(1_000_000); // deadlines are absolute; keep away from timestamp 0
        vm.deal(buyer, 100 ether);
        vm.deal(stranger, 100 ether);
    }

    // ------------------------------------------------------------- helpers

    /// @dev THE master invariant: contract balance == Σ amount of Locked escrows.
    ///      If this holds, funds can be neither lost nor double-spent.
    function _assertSolvent() internal view {
        uint256 sum;
        uint256 n = escrow.nextId();
        for (uint256 i; i < n; ++i) {
            (,, uint256 amount,,,, PaymentEscrow.Status status) = escrow.escrows(i);
            if (status == PaymentEscrow.Status.Locked) sum += amount;
        }
        assertEq(address(escrow).balance, sum, "solvency broken");
    }

    function _lock() internal returns (uint256 id) {
        _assertSolvent();
        vm.prank(buyer);
        id = escrow.lock{value: AMOUNT}(merchant, HASH, TTL);
        _assertSolvent();
    }

    function _status(uint256 id) internal view returns (PaymentEscrow.Status s) {
        (,,,,,, s) = escrow.escrows(id);
    }

    function _deadlines(uint256 id) internal view returns (uint64 rd, uint64 cd) {
        (,,,, rd, cd,) = escrow.escrows(id);
    }

    // ---------------------------------------------------------------- lock

    function test_lock_storesEscrowAndCustodiesFunds() public {
        uint256 id = _lock();

        (
            address b,
            address m,
            uint256 amount,
            bytes32 hash,
            uint64 rd,
            uint64 cd,
            PaymentEscrow.Status status
        ) = escrow.escrows(id);

        assertEq(id, 0);
        assertEq(b, buyer);
        assertEq(m, merchant);
        assertEq(amount, AMOUNT);
        assertEq(hash, HASH);
        assertEq(rd, uint64(block.timestamp) + TTL);
        assertEq(cd, uint64(block.timestamp) + 2 * TTL);
        assertTrue(status == PaymentEscrow.Status.Locked);
        assertEq(address(escrow).balance, AMOUNT);
        assertEq(escrow.nextId(), 1);
    }

    function test_lock_emitsEventWithIndexedBuyerAndMerchant() public {
        // All three topics checked: the sidecar filters on buyer to discover work.
        vm.expectEmit(true, true, true, true, address(escrow));
        emit PaymentEscrow.EscrowLocked(
            0,
            buyer,
            merchant,
            AMOUNT,
            HASH,
            uint64(block.timestamp) + TTL,
            uint64(block.timestamp) + 2 * TTL
        );
        vm.prank(buyer);
        escrow.lock{value: AMOUNT}(merchant, HASH, TTL);
    }

    function test_lock_idsAreSequential() public {
        assertEq(_lock(), 0);
        assertEq(_lock(), 1);
        assertEq(_lock(), 2);
        _assertSolvent();
    }

    /// @dev Boundary for `releaseDeadline < claimDeadline`: ttl == 0 makes them equal.
    function test_lock_revertsWhenDeadlinesWouldNotBeOrdered() public {
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.DeadlineOrder.selector);
        escrow.lock{value: AMOUNT}(merchant, HASH, 0);
    }

    function test_lock_smallestValidTtlIsOne() public {
        vm.prank(buyer);
        uint256 id = escrow.lock{value: AMOUNT}(merchant, HASH, 1);
        (uint64 rd, uint64 cd) = _deadlines(id);
        assertLt(rd, cd);
        assertEq(cd - rd, 1);
    }

    function test_lock_revertsAboveMaxTtl() public {
        uint64 maxTtl = escrow.MAX_TTL(); // read first: expectRevert binds to the very next call
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.TtlTooLong.selector);
        escrow.lock{value: AMOUNT}(merchant, HASH, maxTtl + 1);

        vm.prank(buyer);
        escrow.lock{value: AMOUNT}(merchant, HASH, maxTtl); // boundary is inclusive
    }

    function test_lock_revertsOnZeroValue() public {
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.ZeroAmount.selector);
        escrow.lock{value: 0}(merchant, HASH, TTL);
    }

    function test_lock_revertsOnZeroMerchant() public {
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.ZeroMerchant.selector);
        escrow.lock{value: AMOUNT}(address(0), HASH, TTL);
    }

    function test_contractRejectsBareTransfers() public {
        // No receive/fallback: the balance can only ever move through lock().
        vm.prank(buyer);
        (bool ok,) = address(escrow).call{value: 1 ether}("");
        assertFalse(ok);
        _assertSolvent();
    }

    function testFuzz_lock(uint96 amount, uint64 ttl, address m) public {
        vm.assume(amount > 0 && m != address(0));
        ttl = uint64(bound(ttl, 1, escrow.MAX_TTL()));
        vm.deal(buyer, amount);

        vm.prank(buyer);
        uint256 id = escrow.lock{value: amount}(m, HASH, ttl);
        (uint64 rd, uint64 cd) = _deadlines(id);
        assertLt(rd, cd);
        _assertSolvent();
    }

    // ------------------------------------------------------------- release

    function test_release_paysMerchantInFull() public {
        uint256 id = _lock();
        uint256 before = merchant.balance;

        vm.expectEmit(true, true, false, true, address(escrow));
        emit PaymentEscrow.EscrowReleased(id, merchant, AMOUNT);
        vm.prank(buyer);
        escrow.release(id);

        assertEq(merchant.balance, before + AMOUNT);
        assertEq(address(escrow).balance, 0);
        assertTrue(_status(id) == PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_release_onlyBuyer() public {
        uint256 id = _lock();

        vm.prank(stranger);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.release(id);

        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.release(id);

        _assertSolvent();
    }

    function test_release_worksBeforeAndAfterDeadlines() public {
        uint256 id = _lock();
        (, uint64 cd) = _deadlines(id);
        vm.warp(cd + 1 days); // buyer keeps the right to settle honestly, always
        vm.prank(buyer);
        escrow.release(id);
        assertTrue(_status(id) == PaymentEscrow.Status.Released);
    }

    function test_release_revertsOnNonexistentEscrow() public {
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector); // unset escrow has buyer == address(0)
        escrow.release(999);
    }

    function test_release_revertsWhenMerchantRejectsFunds() public {
        Rejector r = new Rejector();
        vm.prank(buyer);
        uint256 id = escrow.lock{value: AMOUNT}(address(r), HASH, TTL);

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.TransferFailed.selector);
        escrow.release(id);

        assertTrue(_status(id) == PaymentEscrow.Status.Locked);
        _assertSolvent();
    }

    // -------------------------------------------------------------- refund

    function test_refund_revertsOneSecondBeforeDeadlineAndPassesOnIt() public {
        uint256 id = _lock();
        (uint64 rd,) = _deadlines(id);

        vm.warp(rd - 1);
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.TooEarly.selector);
        escrow.refund(id);

        vm.warp(rd);
        uint256 before = buyer.balance;
        vm.expectEmit(true, true, false, true, address(escrow));
        emit PaymentEscrow.EscrowRefunded(id, buyer, AMOUNT);
        vm.prank(buyer);
        escrow.refund(id);

        assertEq(buyer.balance, before + AMOUNT);
        assertTrue(_status(id) == PaymentEscrow.Status.Refunded);
        _assertSolvent();
    }

    function test_refund_onlyBuyer() public {
        uint256 id = _lock();
        (uint64 rd,) = _deadlines(id);
        vm.warp(rd);

        vm.prank(stranger);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.refund(id);

        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.refund(id);

        _assertSolvent();
    }

    // --------------------------------------------------------------- claim

    function test_claim_revertsOneSecondBeforeDeadlineAndPassesOnIt() public {
        uint256 id = _lock();
        (, uint64 cd) = _deadlines(id);

        vm.warp(cd - 1);
        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.TooEarly.selector);
        escrow.claim(id);

        vm.warp(cd);
        uint256 before = merchant.balance;
        vm.expectEmit(true, true, false, true, address(escrow));
        emit PaymentEscrow.EscrowClaimed(id, merchant, AMOUNT);
        vm.prank(merchant);
        escrow.claim(id);

        assertEq(merchant.balance, before + AMOUNT);
        assertTrue(_status(id) == PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_claim_onlyMerchant() public {
        uint256 id = _lock();
        (, uint64 cd) = _deadlines(id);
        vm.warp(cd);

        vm.prank(stranger);
        vm.expectRevert(PaymentEscrow.NotMerchant.selector);
        escrow.claim(id);

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.NotMerchant.selector);
        escrow.claim(id);

        _assertSolvent();
    }

    /// @dev Between releaseDeadline and claimDeadline both parties have a live
    ///      option; whoever acts first wins and the loser gets NotLocked.
    function test_refundAndClaimWindowsOverlap_firstMoverWins() public {
        uint256 id = _lock();
        (uint64 rd, uint64 cd) = _deadlines(id);
        vm.warp(rd);

        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.TooEarly.selector);
        escrow.claim(id);

        vm.prank(buyer);
        escrow.refund(id);

        vm.warp(cd);
        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.claim(id);

        _assertSolvent();
    }

    // ------------------------------------------------------- releasePartial

    function test_releasePartial_splitsExactlyWithNoResidue() public {
        uint256 id = _lock();
        uint256 part = 0.3 ether;
        uint256 mBefore = merchant.balance;
        uint256 bBefore = buyer.balance;

        vm.expectEmit(true, true, false, true, address(escrow));
        emit PaymentEscrow.EscrowPartial(id, merchant, part, AMOUNT - part);
        vm.prank(buyer);
        escrow.releasePartial(id, part);

        uint256 merchantPaid = merchant.balance - mBefore;
        uint256 buyerPaid = buyer.balance - bBefore;
        assertEq(merchantPaid, part);
        assertEq(buyerPaid, AMOUNT - part);
        assertEq(merchantPaid + buyerPaid, AMOUNT, "split must be exact");
        assertEq(address(escrow).balance, 0);
        assertTrue(_status(id) == PaymentEscrow.Status.Partial);
        _assertSolvent();
    }

    function test_releasePartial_fullAmountBoundary() public {
        uint256 id = _lock();
        vm.prank(buyer);
        escrow.releasePartial(id, AMOUNT);
        assertEq(merchant.balance, AMOUNT);
        _assertSolvent();
    }

    function test_releasePartial_zeroAmountReturnsEverything() public {
        uint256 id = _lock();
        uint256 bBefore = buyer.balance;
        vm.prank(buyer);
        escrow.releasePartial(id, 0);
        assertEq(buyer.balance, bBefore + AMOUNT);
        assertEq(merchant.balance, 0);
        _assertSolvent();
    }

    function test_releasePartial_revertsAboveEscrowAmount() public {
        uint256 id = _lock();
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.AmountExceedsEscrow.selector);
        escrow.releasePartial(id, AMOUNT + 1);
        _assertSolvent();
    }

    function test_releasePartial_onlyBuyer() public {
        uint256 id = _lock();

        vm.prank(stranger);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.releasePartial(id, 1);

        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.releasePartial(id, 1);

        _assertSolvent();
    }

    function testFuzz_releasePartial_conservesValue(uint256 part) public {
        uint256 id = _lock();
        part = bound(part, 0, AMOUNT);
        uint256 mBefore = merchant.balance;
        uint256 bBefore = buyer.balance;

        vm.prank(buyer);
        escrow.releasePartial(id, part);

        assertEq((merchant.balance - mBefore) + (buyer.balance - bBefore), AMOUNT);
        _assertSolvent();
    }

    // ---------------------------------------------------- terminal statuses

    function test_terminal_released_rejectsEveryEntrypoint() public {
        uint256 id = _lock();
        vm.prank(buyer);
        escrow.release(id);
        _assertAllEntrypointsRejected(id);
    }

    function test_terminal_refunded_rejectsEveryEntrypoint() public {
        uint256 id = _lock();
        (uint64 rd,) = _deadlines(id);
        vm.warp(rd);
        vm.prank(buyer);
        escrow.refund(id);
        _assertAllEntrypointsRejected(id);
    }

    function test_terminal_partial_rejectsEveryEntrypoint() public {
        uint256 id = _lock();
        vm.prank(buyer);
        escrow.releasePartial(id, 0.5 ether);
        _assertAllEntrypointsRejected(id);
    }

    function _assertAllEntrypointsRejected(uint256 id) internal {
        (, uint64 cd) = _deadlines(id);
        vm.warp(cd + 1); // every timing precondition satisfied; only status blocks
        uint256[] memory ids = new uint256[](1);
        ids[0] = id;

        vm.startPrank(buyer);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.release(id);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.refund(id);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.releasePartial(id, 1);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.releaseMany(ids);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.refundMany(ids);
        vm.stopPrank();

        vm.prank(merchant);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.claim(id);

        _assertSolvent();
    }

    // ------------------------------------------------------------ lockBatch

    function _batch(uint256 n) internal pure returns (bytes32[] memory h, uint256[] memory a) {
        h = new bytes32[](n);
        a = new uint256[](n);
        for (uint256 i; i < n; ++i) {
            h[i] = keccak256(abi.encode(i));
            a[i] = (i + 1) * 0.1 ether;
        }
    }

    function _sum(uint256[] memory a) internal pure returns (uint256 s) {
        for (uint256 i; i < a.length; ++i) {
            s += a[i];
        }
    }

    function test_lockBatch_createsAllEscrows() public {
        (bytes32[] memory h, uint256[] memory a) = _batch(5);
        uint256 total = _sum(a);

        vm.prank(buyer);
        uint256[] memory ids = escrow.lockBatch{value: total}(merchant, h, a, TTL);

        assertEq(ids.length, 5);
        assertEq(escrow.nextId(), 5);
        assertEq(address(escrow).balance, total);
        for (uint256 i; i < 5; ++i) {
            (address b, address m, uint256 amount, bytes32 hash,,, PaymentEscrow.Status s) =
                escrow.escrows(ids[i]);
            assertEq(b, buyer);
            assertEq(m, merchant);
            assertEq(amount, a[i]);
            assertEq(hash, h[i]);
            assertTrue(s == PaymentEscrow.Status.Locked);
        }
        _assertSolvent();
    }

    function test_lockBatch_revertsOnLengthMismatch() public {
        (bytes32[] memory h,) = _batch(3);
        (, uint256[] memory a) = _batch(2);

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.LengthMismatch.selector);
        escrow.lockBatch{value: _sum(a)}(merchant, h, a, TTL);
        _assertSolvent();
    }

    function test_lockBatch_revertsOnValueMismatch() public {
        (bytes32[] memory h, uint256[] memory a) = _batch(3);
        uint256 total = _sum(a);

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.ValueMismatch.selector);
        escrow.lockBatch{value: total - 1}(merchant, h, a, TTL);

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.ValueMismatch.selector);
        escrow.lockBatch{value: total + 1}(merchant, h, a, TTL);

        assertEq(escrow.nextId(), 0, "nothing may survive a reverted batch");
        _assertSolvent();
    }

    function test_lockBatch_revertsOnEmptyAndOversizedArrays() public {
        bytes32[] memory h0 = new bytes32[](0);
        uint256[] memory a0 = new uint256[](0);
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.EmptyBatch.selector);
        escrow.lockBatch{value: 0}(merchant, h0, a0, TTL);

        (bytes32[] memory h, uint256[] memory a) = _batch(escrow.MAX_BATCH() + 1);
        vm.deal(buyer, 1000 ether);
        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.BatchTooLarge.selector);
        escrow.lockBatch{value: _sum(a)}(merchant, h, a, TTL);
        _assertSolvent();
    }

    /// @dev All-or-nothing: one bad element unwinds the whole batch.
    function test_lockBatch_isAllOrNothing() public {
        (bytes32[] memory h, uint256[] memory a) = _batch(3);
        a[1] = 0; // zero-amount element

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.ZeroAmount.selector);
        escrow.lockBatch{value: _sum(a)}(merchant, h, a, TTL);

        assertEq(escrow.nextId(), 0);
        assertEq(address(escrow).balance, 0);
        _assertSolvent();
    }

    function test_lockBatch_atMaxSize() public {
        uint256 n = escrow.MAX_BATCH();
        (bytes32[] memory h, uint256[] memory a) = _batch(n);
        vm.deal(buyer, 1000 ether);

        vm.prank(buyer);
        escrow.lockBatch{value: _sum(a)}(merchant, h, a, TTL);
        assertEq(escrow.nextId(), n);
        _assertSolvent();
    }

    // ------------------------------------------------- releaseMany/refundMany

    function _lockMany(uint256 n) internal returns (uint256[] memory ids) {
        (bytes32[] memory h, uint256[] memory a) = _batch(n);
        vm.prank(buyer);
        ids = escrow.lockBatch{value: _sum(a)}(merchant, h, a, TTL);
    }

    function test_releaseMany_paysEveryMerchantAndKeepsSolvency() public {
        uint256[] memory ids = _lockMany(5);
        uint256 total = address(escrow).balance;

        vm.prank(buyer);
        escrow.releaseMany(ids);

        assertEq(merchant.balance, total);
        assertEq(address(escrow).balance, 0);
        for (uint256 i; i < ids.length; ++i) {
            assertTrue(_status(ids[i]) == PaymentEscrow.Status.Released);
        }
        _assertSolvent();
    }

    function test_refundMany_returnsEverythingToBuyer() public {
        uint256[] memory ids = _lockMany(5);
        uint256 total = address(escrow).balance;
        uint256 before = buyer.balance;
        (uint64 rd,) = _deadlines(ids[0]);
        vm.warp(rd);

        vm.prank(buyer);
        escrow.refundMany(ids);

        assertEq(buyer.balance, before + total);
        assertEq(address(escrow).balance, 0);
        _assertSolvent();
    }

    /// @dev A single ineligible id unwinds the whole batch; no partial settlement.
    function test_releaseMany_isAllOrNothing() public {
        uint256[] memory ids = _lockMany(3);
        uint256 total = address(escrow).balance;

        vm.prank(buyer);
        escrow.release(ids[1]); // already terminal

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.releaseMany(ids);

        assertTrue(_status(ids[0]) == PaymentEscrow.Status.Locked);
        assertTrue(_status(ids[2]) == PaymentEscrow.Status.Locked);
        assertEq(address(escrow).balance, total - _amountOf(ids[1]));
        _assertSolvent();
    }

    function _amountOf(uint256 id) internal view returns (uint256 amount) {
        (,, amount,,,,) = escrow.escrows(id);
    }

    function test_refundMany_isAllOrNothingOnTiming() public {
        uint256[] memory ids = _lockMany(3);
        uint256 total = address(escrow).balance;

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.TooEarly.selector);
        escrow.refundMany(ids);

        assertEq(address(escrow).balance, total);
        _assertSolvent();
    }

    function test_batchPayouts_rejectStrangers() public {
        uint256[] memory ids = _lockMany(2);

        vm.prank(stranger);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.releaseMany(ids);

        vm.prank(stranger);
        vm.expectRevert(PaymentEscrow.NotBuyer.selector);
        escrow.refundMany(ids);

        _assertSolvent();
    }

    function test_batchPayouts_rejectEmptyAndOversized() public {
        uint256[] memory empty = new uint256[](0);
        uint256[] memory big = new uint256[](escrow.MAX_BATCH() + 1);

        vm.startPrank(buyer);
        vm.expectRevert(PaymentEscrow.EmptyBatch.selector);
        escrow.releaseMany(empty);
        vm.expectRevert(PaymentEscrow.BatchTooLarge.selector);
        escrow.releaseMany(big);
        vm.expectRevert(PaymentEscrow.EmptyBatch.selector);
        escrow.refundMany(empty);
        vm.expectRevert(PaymentEscrow.BatchTooLarge.selector);
        escrow.refundMany(big);
        vm.stopPrank();
    }

    /// @dev Duplicate ids in one batch must not double-pay.
    function test_releaseMany_duplicateIdReverts() public {
        uint256 id = _lock();
        uint256[] memory ids = new uint256[](2);
        ids[0] = id;
        ids[1] = id;

        vm.prank(buyer);
        vm.expectRevert(PaymentEscrow.NotLocked.selector);
        escrow.releaseMany(ids);

        assertEq(address(escrow).balance, AMOUNT);
        _assertSolvent();
    }

    // ---------------------------------------------------------- reentrancy

    /// @dev The attacker is both buyer and merchant of the escrow it attacks, so
    ///      every access-control check passes and the guard is the only defence.
    function _armedEscrow(Reenterer.Mode mode) internal returns (Reenterer a, uint256 id) {
        a = new Reenterer(escrow);
        vm.deal(address(a), AMOUNT);
        id = a.lock(address(a), HASH, TTL, AMOUNT);
        a.arm(mode, id);
        _assertSolvent();
    }

    function _assertBlocked(Reenterer a, uint256 id, PaymentEscrow.Status expected) internal view {
        assertGt(a.reentries(), 0, "receive() never ran - test proves nothing");
        assertEq(bytes4(a.lastError()), PaymentEscrow.Reentrancy.selector, "guard did not fire");
        assertTrue(_status(id) == expected);
        assertEq(address(escrow).balance, 0);
        assertEq(address(a).balance, AMOUNT, "attacker took more than it locked");
    }

    function test_reentrancy_releaseIntoRelease() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Release);
        a.callRelease(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_releaseIntoRefund() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Refund);
        (uint64 rd,) = _deadlines(id);
        vm.warp(rd); // refund's timing precondition met: only the guard blocks it
        a.callRelease(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_releaseIntoReleasePartial() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.ReleasePartial);
        a.callRelease(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_releaseIntoClaim() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Claim);
        (, uint64 cd) = _deadlines(id);
        vm.warp(cd);
        a.callRelease(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_refundIntoRefund() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Refund);
        (uint64 rd,) = _deadlines(id);
        vm.warp(rd);
        a.callRefund(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Refunded);
        _assertSolvent();
    }

    function test_reentrancy_refundIntoRelease() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Release);
        (uint64 rd,) = _deadlines(id);
        vm.warp(rd);
        a.callRefund(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Refunded);
        _assertSolvent();
    }

    function test_reentrancy_releasePartialIntoReleasePartial() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.ReleasePartial);
        a.callReleasePartial(id, 0.4 ether);
        _assertBlocked(a, id, PaymentEscrow.Status.Partial);
        assertEq(a.reentries(), 2, "both legs of the split must hit the guard");
        _assertSolvent();
    }

    function test_reentrancy_releasePartialIntoRelease() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Release);
        a.callReleasePartial(id, 0.4 ether);
        _assertBlocked(a, id, PaymentEscrow.Status.Partial);
        _assertSolvent();
    }

    function test_reentrancy_claimIntoClaim() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Claim);
        (, uint64 cd) = _deadlines(id);
        vm.warp(cd);
        a.callClaim(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_claimIntoRefund() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Refund);
        (, uint64 cd) = _deadlines(id);
        vm.warp(cd);
        a.callClaim(id);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_releaseManyIntoReleaseMany() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.ReleaseMany);
        uint256[] memory ids = new uint256[](1);
        ids[0] = id;
        a.callReleaseMany(ids);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_releaseManyIntoRelease() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.Release);
        uint256[] memory ids = new uint256[](1);
        ids[0] = id;
        a.callReleaseMany(ids);
        _assertBlocked(a, id, PaymentEscrow.Status.Released);
        _assertSolvent();
    }

    function test_reentrancy_refundManyIntoRefundMany() public {
        (Reenterer a, uint256 id) = _armedEscrow(Reenterer.Mode.RefundMany);
        (uint64 rd,) = _deadlines(id);
        vm.warp(rd);
        uint256[] memory ids = new uint256[](1);
        ids[0] = id;
        a.callRefundMany(ids);
        _assertBlocked(a, id, PaymentEscrow.Status.Refunded);
        _assertSolvent();
    }

    /// @dev Mid-batch re-entry: the attacker's escrow is element 0, so the guard
    ///      must hold while later elements of the same batch are still Locked.
    function test_reentrancy_midBatchCannotTouchLaterElements() public {
        (Reenterer a, uint256 id0) = _armedEscrow(Reenterer.Mode.Release);
        vm.deal(address(a), AMOUNT);
        uint256 id1 = a.lock(address(a), HASH, TTL, AMOUNT);
        a.arm(Reenterer.Mode.Release, id1); // attack the element not yet settled

        uint256[] memory ids = new uint256[](2);
        ids[0] = id0;
        ids[1] = id1;
        a.callReleaseMany(ids);

        assertGt(a.reentries(), 0);
        assertEq(bytes4(a.lastError()), PaymentEscrow.Reentrancy.selector);
        assertEq(address(escrow).balance, 0);
        assertEq(address(a).balance, 2 * AMOUNT);
        _assertSolvent();
    }

    // ------------------------------------------------- cross-escrow solvency

    /// @dev Many buyers, mixed outcomes: nobody's settlement touches anyone else's
    ///      principal. This is the singleton's whole safety story.
    function test_multipleBuyers_settlementsAreIsolated() public {
        address buyer2 = makeAddr("buyer2");
        vm.deal(buyer2, 10 ether);

        vm.prank(buyer);
        uint256 a1 = escrow.lock{value: 1 ether}(merchant, HASH, TTL);
        vm.prank(buyer2);
        uint256 b1 = escrow.lock{value: 2 ether}(merchant, HASH, TTL);
        vm.prank(buyer);
        uint256 a2 = escrow.lock{value: 3 ether}(stranger, HASH, TTL);
        _assertSolvent();

        vm.prank(buyer);
        escrow.release(a1);
        _assertSolvent();

        vm.prank(buyer);
        escrow.releasePartial(a2, 1 ether);
        _assertSolvent();

        (uint64 rd,) = _deadlines(b1);
        vm.warp(rd);
        vm.prank(buyer2);
        escrow.refund(b1);

        assertEq(address(escrow).balance, 0);
        assertEq(buyer2.balance, 10 ether, "buyer2 made whole");
        _assertSolvent();
    }
}
