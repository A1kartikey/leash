// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title PaymentEscrow
/// @notice Public singleton escrow for agent->merchant payments in native MON.
///         No owner, no admin, no fee, no pause, no upgrade path. A bug means a
///         v2 at a new address and a migration; it never means an admin fix.
/// @dev    Access control is exactly `msg.sender == e.buyer` (or e.merchant for
///         claim). There is no delegate, registry or approval flow: Leash runs
///         with the buyer's own key.
contract PaymentEscrow {
    enum Status {
        Locked, // 0 - default; a nonexistent escrow reads as Locked with buyer == address(0)
        Released,
        Refunded,
        Partial
    }

    struct Escrow {
        address buyer;
        address merchant;
        uint256 amount;
        bytes32 resourceHash;
        uint64 releaseDeadline;
        uint64 claimDeadline;
        Status status;
    }

    /// @dev Caps batch gas. Anything larger is the caller's job to split.
    uint256 public constant MAX_BATCH = 50;
    /// @dev Bounds the uint64 deadline arithmetic and stops absurd locks.
    uint64 public constant MAX_TTL = 365 days;

    mapping(uint256 => Escrow) public escrows;
    uint256 public nextId;

    /// @dev buyer AND merchant indexed: the off-chain sidecar discovers its
    ///      escrows by filtering on buyer. Do not un-index either.
    event EscrowLocked(
        uint256 indexed id,
        address indexed buyer,
        address indexed merchant,
        uint256 amount,
        bytes32 resourceHash,
        uint64 releaseDeadline,
        uint64 claimDeadline
    );
    event EscrowReleased(uint256 indexed id, address indexed merchant, uint256 amount);
    event EscrowRefunded(uint256 indexed id, address indexed buyer, uint256 amount);
    event EscrowPartial(
        uint256 indexed id, address indexed merchant, uint256 merchantAmount, uint256 buyerAmount
    );
    event EscrowClaimed(uint256 indexed id, address indexed merchant, uint256 amount);

    error NotBuyer();
    error NotMerchant();
    error NotLocked();
    error ZeroAmount();
    error ZeroMerchant();
    error DeadlineOrder();
    error TtlTooLong();
    error TooEarly();
    error AmountExceedsEscrow();
    error LengthMismatch();
    error BatchTooLarge();
    error EmptyBatch();
    error ValueMismatch();
    error TransferFailed();
    error Reentrancy();

    uint256 private _entered = 1;

    modifier nonReentrant() {
        if (_entered != 1) revert Reentrancy();
        _entered = 2;
        _;
        _entered = 1;
    }

    // ---------------------------------------------------------------- lock

    function lock(address merchant, bytes32 resourceHash, uint64 ttl)
        external
        payable
        returns (uint256 id)
    {
        id = _lock(merchant, resourceHash, msg.value, ttl);
    }

    /// @notice All-or-nothing. `msg.value` must equal the sum of `amounts` exactly.
    function lockBatch(
        address merchant,
        bytes32[] calldata hashes,
        uint256[] calldata amounts,
        uint64 ttl
    ) external payable returns (uint256[] memory ids) {
        if (hashes.length != amounts.length) revert LengthMismatch();
        if (hashes.length == 0) revert EmptyBatch();
        if (hashes.length > MAX_BATCH) revert BatchTooLarge();

        ids = new uint256[](hashes.length);
        uint256 total;
        for (uint256 i; i < hashes.length; ++i) {
            total += amounts[i];
            ids[i] = _lock(merchant, hashes[i], amounts[i], ttl);
        }
        if (total != msg.value) revert ValueMismatch();
    }

    function _lock(address merchant, bytes32 resourceHash, uint256 amount, uint64 ttl)
        private
        returns (uint256 id)
    {
        if (amount == 0) revert ZeroAmount();
        if (merchant == address(0)) revert ZeroMerchant();
        if (ttl > MAX_TTL) revert TtlTooLong();

        uint64 releaseDeadline = uint64(block.timestamp) + ttl;
        uint64 claimDeadline = releaseDeadline + ttl;
        if (releaseDeadline >= claimDeadline) revert DeadlineOrder();

        id = nextId++;
        escrows[id] = Escrow({
            buyer: msg.sender,
            merchant: merchant,
            amount: amount,
            resourceHash: resourceHash,
            releaseDeadline: releaseDeadline,
            claimDeadline: claimDeadline,
            status: Status.Locked
        });

        emit EscrowLocked(
            id, msg.sender, merchant, amount, resourceHash, releaseDeadline, claimDeadline
        );
    }

    // ------------------------------------------------------------- payouts

    function release(uint256 id) external nonReentrant {
        _release(id);
    }

    function releaseMany(uint256[] calldata ids) external nonReentrant {
        _checkBatch(ids.length);
        for (uint256 i; i < ids.length; ++i) {
            _release(ids[i]);
        }
    }

    function refund(uint256 id) external nonReentrant {
        _refund(id);
    }

    function refundMany(uint256[] calldata ids) external nonReentrant {
        _checkBatch(ids.length);
        for (uint256 i; i < ids.length; ++i) {
            _refund(ids[i]);
        }
    }

    /// @notice Pays `amount` to the merchant and the remainder back to the buyer
    ///         in the same transaction. Terminal either way.
    function releasePartial(uint256 id, uint256 amount) external nonReentrant {
        Escrow storage e = escrows[id];
        if (msg.sender != e.buyer) revert NotBuyer();
        if (e.status != Status.Locked) revert NotLocked();
        if (amount > e.amount) revert AmountExceedsEscrow();

        uint256 remainder = e.amount - amount;
        e.status = Status.Partial; // effects before interactions

        emit EscrowPartial(id, e.merchant, amount, remainder);
        _pay(e.merchant, amount);
        _pay(e.buyer, remainder);
    }

    /// @notice Merchant's backstop for a buyer sidecar that never came back.
    function claim(uint256 id) external nonReentrant {
        Escrow storage e = escrows[id];
        if (msg.sender != e.merchant) revert NotMerchant();
        if (e.status != Status.Locked) revert NotLocked();
        if (block.timestamp < e.claimDeadline) revert TooEarly();

        uint256 amount = e.amount;
        e.status = Status.Released; // effects before interactions

        emit EscrowClaimed(id, e.merchant, amount);
        _pay(e.merchant, amount);
    }

    function _release(uint256 id) private {
        Escrow storage e = escrows[id];
        if (msg.sender != e.buyer) revert NotBuyer();
        if (e.status != Status.Locked) revert NotLocked();

        uint256 amount = e.amount;
        e.status = Status.Released; // effects before interactions

        emit EscrowReleased(id, e.merchant, amount);
        _pay(e.merchant, amount);
    }

    function _refund(uint256 id) private {
        Escrow storage e = escrows[id];
        if (msg.sender != e.buyer) revert NotBuyer();
        if (e.status != Status.Locked) revert NotLocked();
        if (block.timestamp < e.releaseDeadline) revert TooEarly();

        uint256 amount = e.amount;
        e.status = Status.Refunded; // effects before interactions

        emit EscrowRefunded(id, e.buyer, amount);
        _pay(e.buyer, amount);
    }

    function _checkBatch(uint256 len) private pure {
        if (len == 0) revert EmptyBatch();
        if (len > MAX_BATCH) revert BatchTooLarge();
    }

    function _pay(address to, uint256 amount) private {
        if (amount == 0) return; // a 0-value call to a reverting receiver must not brick a partial
        (bool ok,) = to.call{value: amount}("");
        if (!ok) revert TransferFailed();
    }
}
