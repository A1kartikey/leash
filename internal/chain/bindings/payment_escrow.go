// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
	_ = time.Tick
	_ = context.Background
)

// PaymentEscrowMetaData contains all meta data concerning the PaymentEscrow contract.
var PaymentEscrowMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"MAX_BATCH\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_TTL\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"claim\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"escrows\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"buyer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"merchant\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"resourceHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"releaseDeadline\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimDeadline\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"status\",\"type\":\"uint8\",\"internalType\":\"enumPaymentEscrow.Status\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"lock\",\"inputs\":[{\"name\":\"merchant\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"resourceHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"ttl\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"lockBatch\",\"inputs\":[{\"name\":\"merchant\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"hashes\",\"type\":\"bytes32[]\",\"internalType\":\"bytes32[]\"},{\"name\":\"amounts\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"ttl\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[{\"name\":\"ids\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"nextId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"refund\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"refundMany\",\"inputs\":[{\"name\":\"ids\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"release\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"releaseMany\",\"inputs\":[{\"name\":\"ids\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"releasePartial\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"EscrowClaimed\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"merchant\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EscrowLocked\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"buyer\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"merchant\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"resourceHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"releaseDeadline\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"claimDeadline\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EscrowPartial\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"merchant\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"merchantAmount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"buyerAmount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EscrowRefunded\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"buyer\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EscrowReleased\",\"inputs\":[{\"name\":\"id\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"merchant\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AmountExceedsEscrow\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"BatchTooLarge\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"DeadlineOrder\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"EmptyBatch\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"LengthMismatch\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotBuyer\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotLocked\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotMerchant\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Reentrancy\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"TooEarly\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"TransferFailed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"TtlTooLong\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ValueMismatch\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ZeroAmount\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ZeroMerchant\",\"inputs\":[]}]",
}

// PaymentEscrowABI is the input ABI used to generate the binding from.
// Deprecated: Use PaymentEscrowMetaData.ABI instead.
var PaymentEscrowABI = PaymentEscrowMetaData.ABI

// PaymentEscrow is an auto generated Go binding around an Ethereum contract.
type PaymentEscrow struct {
	PaymentEscrowCaller     // Read-only binding to the contract
	PaymentEscrowTransactor // Write-only binding to the contract
	PaymentEscrowFilterer   // Log filterer for contract events
}

// PaymentEscrowCaller is an auto generated read-only Go binding around an Ethereum contract.
type PaymentEscrowCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PaymentEscrowTransactor is an auto generated write-only Go binding around an Ethereum contract.
type PaymentEscrowTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PaymentEscrowFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type PaymentEscrowFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PaymentEscrowSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type PaymentEscrowSession struct {
	Contract     *PaymentEscrow    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// PaymentEscrowCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type PaymentEscrowCallerSession struct {
	Contract *PaymentEscrowCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// PaymentEscrowTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type PaymentEscrowTransactorSession struct {
	Contract     *PaymentEscrowTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// PaymentEscrowRaw is an auto generated low-level Go binding around an Ethereum contract.
type PaymentEscrowRaw struct {
	Contract *PaymentEscrow // Generic contract binding to access the raw methods on
}

// PaymentEscrowCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type PaymentEscrowCallerRaw struct {
	Contract *PaymentEscrowCaller // Generic read-only contract binding to access the raw methods on
}

// PaymentEscrowTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type PaymentEscrowTransactorRaw struct {
	Contract *PaymentEscrowTransactor // Generic write-only contract binding to access the raw methods on
}

// NewPaymentEscrow creates a new instance of PaymentEscrow, bound to a specific deployed contract.
func NewPaymentEscrow(address common.Address, backend bind.ContractBackend) (*PaymentEscrow, error) {
	contract, err := bindPaymentEscrow(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrow{PaymentEscrowCaller: PaymentEscrowCaller{contract: contract}, PaymentEscrowTransactor: PaymentEscrowTransactor{contract: contract}, PaymentEscrowFilterer: PaymentEscrowFilterer{contract: contract}}, nil
}

// NewPaymentEscrowCaller creates a new read-only instance of PaymentEscrow, bound to a specific deployed contract.
func NewPaymentEscrowCaller(address common.Address, caller bind.ContractCaller) (*PaymentEscrowCaller, error) {
	contract, err := bindPaymentEscrow(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowCaller{contract: contract}, nil
}

// NewPaymentEscrowTransactor creates a new write-only instance of PaymentEscrow, bound to a specific deployed contract.
func NewPaymentEscrowTransactor(address common.Address, transactor bind.ContractTransactor) (*PaymentEscrowTransactor, error) {
	contract, err := bindPaymentEscrow(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowTransactor{contract: contract}, nil
}

// NewPaymentEscrowFilterer creates a new log filterer instance of PaymentEscrow, bound to a specific deployed contract.
func NewPaymentEscrowFilterer(address common.Address, filterer bind.ContractFilterer) (*PaymentEscrowFilterer, error) {
	contract, err := bindPaymentEscrow(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowFilterer{contract: contract}, nil
}

// bindPaymentEscrow binds a generic wrapper to an already deployed contract.
func bindPaymentEscrow(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := PaymentEscrowMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PaymentEscrow *PaymentEscrowRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PaymentEscrow.Contract.PaymentEscrowCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PaymentEscrow *PaymentEscrowRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.PaymentEscrowTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PaymentEscrow *PaymentEscrowRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.PaymentEscrowTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PaymentEscrow *PaymentEscrowCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PaymentEscrow.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PaymentEscrow *PaymentEscrowTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PaymentEscrow *PaymentEscrowTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.contract.Transact(opts, method, params...)
}

// MAXBATCH is a free data retrieval call binding the contract method 0x950bff9f.
//
// Solidity: function MAX_BATCH() view returns(uint256)
func (_PaymentEscrow *PaymentEscrowCaller) MAXBATCH(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PaymentEscrow.contract.Call(opts, &out, "MAX_BATCH")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXBATCH is a free data retrieval call binding the contract method 0x950bff9f.
//
// Solidity: function MAX_BATCH() view returns(uint256)
func (_PaymentEscrow *PaymentEscrowSession) MAXBATCH() (*big.Int, error) {
	return _PaymentEscrow.Contract.MAXBATCH(&_PaymentEscrow.CallOpts)
}

// MAXBATCH is a free data retrieval call binding the contract method 0x950bff9f.
//
// Solidity: function MAX_BATCH() view returns(uint256)
func (_PaymentEscrow *PaymentEscrowCallerSession) MAXBATCH() (*big.Int, error) {
	return _PaymentEscrow.Contract.MAXBATCH(&_PaymentEscrow.CallOpts)
}

// MAXTTL is a free data retrieval call binding the contract method 0xe01b402d.
//
// Solidity: function MAX_TTL() view returns(uint64)
func (_PaymentEscrow *PaymentEscrowCaller) MAXTTL(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _PaymentEscrow.contract.Call(opts, &out, "MAX_TTL")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// MAXTTL is a free data retrieval call binding the contract method 0xe01b402d.
//
// Solidity: function MAX_TTL() view returns(uint64)
func (_PaymentEscrow *PaymentEscrowSession) MAXTTL() (uint64, error) {
	return _PaymentEscrow.Contract.MAXTTL(&_PaymentEscrow.CallOpts)
}

// MAXTTL is a free data retrieval call binding the contract method 0xe01b402d.
//
// Solidity: function MAX_TTL() view returns(uint64)
func (_PaymentEscrow *PaymentEscrowCallerSession) MAXTTL() (uint64, error) {
	return _PaymentEscrow.Contract.MAXTTL(&_PaymentEscrow.CallOpts)
}

// Escrows is a free data retrieval call binding the contract method 0x012f52ee.
//
// Solidity: function escrows(uint256 ) view returns(address buyer, address merchant, uint256 amount, bytes32 resourceHash, uint64 releaseDeadline, uint64 claimDeadline, uint8 status)
func (_PaymentEscrow *PaymentEscrowCaller) Escrows(opts *bind.CallOpts, arg0 *big.Int) (struct {
	Buyer           common.Address
	Merchant        common.Address
	Amount          *big.Int
	ResourceHash    [32]byte
	ReleaseDeadline uint64
	ClaimDeadline   uint64
	Status          uint8
}, error) {
	var out []interface{}
	err := _PaymentEscrow.contract.Call(opts, &out, "escrows", arg0)

	outstruct := new(struct {
		Buyer           common.Address
		Merchant        common.Address
		Amount          *big.Int
		ResourceHash    [32]byte
		ReleaseDeadline uint64
		ClaimDeadline   uint64
		Status          uint8
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Buyer = *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	outstruct.Merchant = *abi.ConvertType(out[1], new(common.Address)).(*common.Address)
	outstruct.Amount = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.ResourceHash = *abi.ConvertType(out[3], new([32]byte)).(*[32]byte)
	outstruct.ReleaseDeadline = *abi.ConvertType(out[4], new(uint64)).(*uint64)
	outstruct.ClaimDeadline = *abi.ConvertType(out[5], new(uint64)).(*uint64)
	outstruct.Status = *abi.ConvertType(out[6], new(uint8)).(*uint8)

	return *outstruct, err

}

// Escrows is a free data retrieval call binding the contract method 0x012f52ee.
//
// Solidity: function escrows(uint256 ) view returns(address buyer, address merchant, uint256 amount, bytes32 resourceHash, uint64 releaseDeadline, uint64 claimDeadline, uint8 status)
func (_PaymentEscrow *PaymentEscrowSession) Escrows(arg0 *big.Int) (struct {
	Buyer           common.Address
	Merchant        common.Address
	Amount          *big.Int
	ResourceHash    [32]byte
	ReleaseDeadline uint64
	ClaimDeadline   uint64
	Status          uint8
}, error) {
	return _PaymentEscrow.Contract.Escrows(&_PaymentEscrow.CallOpts, arg0)
}

// Escrows is a free data retrieval call binding the contract method 0x012f52ee.
//
// Solidity: function escrows(uint256 ) view returns(address buyer, address merchant, uint256 amount, bytes32 resourceHash, uint64 releaseDeadline, uint64 claimDeadline, uint8 status)
func (_PaymentEscrow *PaymentEscrowCallerSession) Escrows(arg0 *big.Int) (struct {
	Buyer           common.Address
	Merchant        common.Address
	Amount          *big.Int
	ResourceHash    [32]byte
	ReleaseDeadline uint64
	ClaimDeadline   uint64
	Status          uint8
}, error) {
	return _PaymentEscrow.Contract.Escrows(&_PaymentEscrow.CallOpts, arg0)
}

// NextId is a free data retrieval call binding the contract method 0x61b8ce8c.
//
// Solidity: function nextId() view returns(uint256)
func (_PaymentEscrow *PaymentEscrowCaller) NextId(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PaymentEscrow.contract.Call(opts, &out, "nextId")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NextId is a free data retrieval call binding the contract method 0x61b8ce8c.
//
// Solidity: function nextId() view returns(uint256)
func (_PaymentEscrow *PaymentEscrowSession) NextId() (*big.Int, error) {
	return _PaymentEscrow.Contract.NextId(&_PaymentEscrow.CallOpts)
}

// NextId is a free data retrieval call binding the contract method 0x61b8ce8c.
//
// Solidity: function nextId() view returns(uint256)
func (_PaymentEscrow *PaymentEscrowCallerSession) NextId() (*big.Int, error) {
	return _PaymentEscrow.Contract.NextId(&_PaymentEscrow.CallOpts)
}

// Claim is a paid mutator transaction binding the contract method 0x379607f5.
//
// Solidity: function claim(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowTransactor) Claim(opts *bind.TransactOpts, id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "claim", id)
}

// Claim is a paid mutator transaction binding the contract method 0x379607f5.
//
// Solidity: function claim(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowSession) Claim(id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Claim(&_PaymentEscrow.TransactOpts, id)
}

// Claim is a paid mutator transaction binding the contract method 0x379607f5.
//
// Solidity: function claim(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowTransactorSession) Claim(id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Claim(&_PaymentEscrow.TransactOpts, id)
}

// Lock is a paid mutator transaction binding the contract method 0x473500ff.
//
// Solidity: function lock(address merchant, bytes32 resourceHash, uint64 ttl) payable returns(uint256 id)
func (_PaymentEscrow *PaymentEscrowTransactor) Lock(opts *bind.TransactOpts, merchant common.Address, resourceHash [32]byte, ttl uint64) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "lock", merchant, resourceHash, ttl)
}

// Lock is a paid mutator transaction binding the contract method 0x473500ff.
//
// Solidity: function lock(address merchant, bytes32 resourceHash, uint64 ttl) payable returns(uint256 id)
func (_PaymentEscrow *PaymentEscrowSession) Lock(merchant common.Address, resourceHash [32]byte, ttl uint64) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Lock(&_PaymentEscrow.TransactOpts, merchant, resourceHash, ttl)
}

// Lock is a paid mutator transaction binding the contract method 0x473500ff.
//
// Solidity: function lock(address merchant, bytes32 resourceHash, uint64 ttl) payable returns(uint256 id)
func (_PaymentEscrow *PaymentEscrowTransactorSession) Lock(merchant common.Address, resourceHash [32]byte, ttl uint64) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Lock(&_PaymentEscrow.TransactOpts, merchant, resourceHash, ttl)
}

// LockBatch is a paid mutator transaction binding the contract method 0x20952c13.
//
// Solidity: function lockBatch(address merchant, bytes32[] hashes, uint256[] amounts, uint64 ttl) payable returns(uint256[] ids)
func (_PaymentEscrow *PaymentEscrowTransactor) LockBatch(opts *bind.TransactOpts, merchant common.Address, hashes [][32]byte, amounts []*big.Int, ttl uint64) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "lockBatch", merchant, hashes, amounts, ttl)
}

// LockBatch is a paid mutator transaction binding the contract method 0x20952c13.
//
// Solidity: function lockBatch(address merchant, bytes32[] hashes, uint256[] amounts, uint64 ttl) payable returns(uint256[] ids)
func (_PaymentEscrow *PaymentEscrowSession) LockBatch(merchant common.Address, hashes [][32]byte, amounts []*big.Int, ttl uint64) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.LockBatch(&_PaymentEscrow.TransactOpts, merchant, hashes, amounts, ttl)
}

// LockBatch is a paid mutator transaction binding the contract method 0x20952c13.
//
// Solidity: function lockBatch(address merchant, bytes32[] hashes, uint256[] amounts, uint64 ttl) payable returns(uint256[] ids)
func (_PaymentEscrow *PaymentEscrowTransactorSession) LockBatch(merchant common.Address, hashes [][32]byte, amounts []*big.Int, ttl uint64) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.LockBatch(&_PaymentEscrow.TransactOpts, merchant, hashes, amounts, ttl)
}

// Refund is a paid mutator transaction binding the contract method 0x278ecde1.
//
// Solidity: function refund(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowTransactor) Refund(opts *bind.TransactOpts, id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "refund", id)
}

// Refund is a paid mutator transaction binding the contract method 0x278ecde1.
//
// Solidity: function refund(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowSession) Refund(id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Refund(&_PaymentEscrow.TransactOpts, id)
}

// Refund is a paid mutator transaction binding the contract method 0x278ecde1.
//
// Solidity: function refund(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowTransactorSession) Refund(id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Refund(&_PaymentEscrow.TransactOpts, id)
}

// RefundMany is a paid mutator transaction binding the contract method 0xca676dcf.
//
// Solidity: function refundMany(uint256[] ids) returns()
func (_PaymentEscrow *PaymentEscrowTransactor) RefundMany(opts *bind.TransactOpts, ids []*big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "refundMany", ids)
}

// RefundMany is a paid mutator transaction binding the contract method 0xca676dcf.
//
// Solidity: function refundMany(uint256[] ids) returns()
func (_PaymentEscrow *PaymentEscrowSession) RefundMany(ids []*big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.RefundMany(&_PaymentEscrow.TransactOpts, ids)
}

// RefundMany is a paid mutator transaction binding the contract method 0xca676dcf.
//
// Solidity: function refundMany(uint256[] ids) returns()
func (_PaymentEscrow *PaymentEscrowTransactorSession) RefundMany(ids []*big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.RefundMany(&_PaymentEscrow.TransactOpts, ids)
}

// Release is a paid mutator transaction binding the contract method 0x37bdc99b.
//
// Solidity: function release(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowTransactor) Release(opts *bind.TransactOpts, id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "release", id)
}

// Release is a paid mutator transaction binding the contract method 0x37bdc99b.
//
// Solidity: function release(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowSession) Release(id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Release(&_PaymentEscrow.TransactOpts, id)
}

// Release is a paid mutator transaction binding the contract method 0x37bdc99b.
//
// Solidity: function release(uint256 id) returns()
func (_PaymentEscrow *PaymentEscrowTransactorSession) Release(id *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.Release(&_PaymentEscrow.TransactOpts, id)
}

// ReleaseMany is a paid mutator transaction binding the contract method 0x0715139b.
//
// Solidity: function releaseMany(uint256[] ids) returns()
func (_PaymentEscrow *PaymentEscrowTransactor) ReleaseMany(opts *bind.TransactOpts, ids []*big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "releaseMany", ids)
}

// ReleaseMany is a paid mutator transaction binding the contract method 0x0715139b.
//
// Solidity: function releaseMany(uint256[] ids) returns()
func (_PaymentEscrow *PaymentEscrowSession) ReleaseMany(ids []*big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.ReleaseMany(&_PaymentEscrow.TransactOpts, ids)
}

// ReleaseMany is a paid mutator transaction binding the contract method 0x0715139b.
//
// Solidity: function releaseMany(uint256[] ids) returns()
func (_PaymentEscrow *PaymentEscrowTransactorSession) ReleaseMany(ids []*big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.ReleaseMany(&_PaymentEscrow.TransactOpts, ids)
}

// ReleasePartial is a paid mutator transaction binding the contract method 0x62e3b2cd.
//
// Solidity: function releasePartial(uint256 id, uint256 amount) returns()
func (_PaymentEscrow *PaymentEscrowTransactor) ReleasePartial(opts *bind.TransactOpts, id *big.Int, amount *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.contract.Transact(opts, "releasePartial", id, amount)
}

// ReleasePartial is a paid mutator transaction binding the contract method 0x62e3b2cd.
//
// Solidity: function releasePartial(uint256 id, uint256 amount) returns()
func (_PaymentEscrow *PaymentEscrowSession) ReleasePartial(id *big.Int, amount *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.ReleasePartial(&_PaymentEscrow.TransactOpts, id, amount)
}

// ReleasePartial is a paid mutator transaction binding the contract method 0x62e3b2cd.
//
// Solidity: function releasePartial(uint256 id, uint256 amount) returns()
func (_PaymentEscrow *PaymentEscrowTransactorSession) ReleasePartial(id *big.Int, amount *big.Int) (*types.Transaction, error) {
	return _PaymentEscrow.Contract.ReleasePartial(&_PaymentEscrow.TransactOpts, id, amount)
}

// PaymentEscrowEscrowClaimedIterator is returned from FilterEscrowClaimed and is used to iterate over the raw logs and unpacked data for EscrowClaimed events raised by the PaymentEscrow contract.
type PaymentEscrowEscrowClaimedIterator struct {
	Event *PaymentEscrowEscrowClaimed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *PaymentEscrowEscrowClaimedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentEscrowEscrowClaimed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(PaymentEscrowEscrowClaimed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *PaymentEscrowEscrowClaimedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentEscrowEscrowClaimedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentEscrowEscrowClaimed represents a EscrowClaimed event raised by the PaymentEscrow contract.
type PaymentEscrowEscrowClaimed struct {
	Id       *big.Int
	Merchant common.Address
	Amount   *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterEscrowClaimed is a free log retrieval operation binding the contract event 0x264b84d6bb26c82423cfcb91b4220efaba80e38b8ea7a87b58a80fcaf0956912.
//
// Solidity: event EscrowClaimed(uint256 indexed id, address indexed merchant, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) FilterEscrowClaimed(opts *bind.FilterOpts, id []*big.Int, merchant []common.Address) (*PaymentEscrowEscrowClaimedIterator, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.FilterLogs(opts, "EscrowClaimed", idRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowEscrowClaimedIterator{contract: _PaymentEscrow.contract, event: "EscrowClaimed", logs: logs, sub: sub}, nil
}

// WatchEscrowClaimed is a free log subscription operation binding the contract event 0x264b84d6bb26c82423cfcb91b4220efaba80e38b8ea7a87b58a80fcaf0956912.
//
// Solidity: event EscrowClaimed(uint256 indexed id, address indexed merchant, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) WatchEscrowClaimed(opts *bind.WatchOpts, sink chan<- *PaymentEscrowEscrowClaimed, id []*big.Int, merchant []common.Address) (event.Subscription, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.WatchLogs(opts, "EscrowClaimed", idRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentEscrowEscrowClaimed)
				if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowClaimed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseEscrowClaimed is a log parse operation binding the contract event 0x264b84d6bb26c82423cfcb91b4220efaba80e38b8ea7a87b58a80fcaf0956912.
//
// Solidity: event EscrowClaimed(uint256 indexed id, address indexed merchant, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) ParseEscrowClaimed(log types.Log) (*PaymentEscrowEscrowClaimed, error) {
	event := new(PaymentEscrowEscrowClaimed)
	if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowClaimed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentEscrowEscrowLockedIterator is returned from FilterEscrowLocked and is used to iterate over the raw logs and unpacked data for EscrowLocked events raised by the PaymentEscrow contract.
type PaymentEscrowEscrowLockedIterator struct {
	Event *PaymentEscrowEscrowLocked // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *PaymentEscrowEscrowLockedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentEscrowEscrowLocked)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(PaymentEscrowEscrowLocked)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *PaymentEscrowEscrowLockedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentEscrowEscrowLockedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentEscrowEscrowLocked represents a EscrowLocked event raised by the PaymentEscrow contract.
type PaymentEscrowEscrowLocked struct {
	Id              *big.Int
	Buyer           common.Address
	Merchant        common.Address
	Amount          *big.Int
	ResourceHash    [32]byte
	ReleaseDeadline uint64
	ClaimDeadline   uint64
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterEscrowLocked is a free log retrieval operation binding the contract event 0x806c39723a11019c5148273aed0b501172f7c69db9d295557053ce9b78a32924.
//
// Solidity: event EscrowLocked(uint256 indexed id, address indexed buyer, address indexed merchant, uint256 amount, bytes32 resourceHash, uint64 releaseDeadline, uint64 claimDeadline)
func (_PaymentEscrow *PaymentEscrowFilterer) FilterEscrowLocked(opts *bind.FilterOpts, id []*big.Int, buyer []common.Address, merchant []common.Address) (*PaymentEscrowEscrowLockedIterator, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var buyerRule []interface{}
	for _, buyerItem := range buyer {
		buyerRule = append(buyerRule, buyerItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.FilterLogs(opts, "EscrowLocked", idRule, buyerRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowEscrowLockedIterator{contract: _PaymentEscrow.contract, event: "EscrowLocked", logs: logs, sub: sub}, nil
}

// WatchEscrowLocked is a free log subscription operation binding the contract event 0x806c39723a11019c5148273aed0b501172f7c69db9d295557053ce9b78a32924.
//
// Solidity: event EscrowLocked(uint256 indexed id, address indexed buyer, address indexed merchant, uint256 amount, bytes32 resourceHash, uint64 releaseDeadline, uint64 claimDeadline)
func (_PaymentEscrow *PaymentEscrowFilterer) WatchEscrowLocked(opts *bind.WatchOpts, sink chan<- *PaymentEscrowEscrowLocked, id []*big.Int, buyer []common.Address, merchant []common.Address) (event.Subscription, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var buyerRule []interface{}
	for _, buyerItem := range buyer {
		buyerRule = append(buyerRule, buyerItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.WatchLogs(opts, "EscrowLocked", idRule, buyerRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentEscrowEscrowLocked)
				if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowLocked", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseEscrowLocked is a log parse operation binding the contract event 0x806c39723a11019c5148273aed0b501172f7c69db9d295557053ce9b78a32924.
//
// Solidity: event EscrowLocked(uint256 indexed id, address indexed buyer, address indexed merchant, uint256 amount, bytes32 resourceHash, uint64 releaseDeadline, uint64 claimDeadline)
func (_PaymentEscrow *PaymentEscrowFilterer) ParseEscrowLocked(log types.Log) (*PaymentEscrowEscrowLocked, error) {
	event := new(PaymentEscrowEscrowLocked)
	if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowLocked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentEscrowEscrowPartialIterator is returned from FilterEscrowPartial and is used to iterate over the raw logs and unpacked data for EscrowPartial events raised by the PaymentEscrow contract.
type PaymentEscrowEscrowPartialIterator struct {
	Event *PaymentEscrowEscrowPartial // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *PaymentEscrowEscrowPartialIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentEscrowEscrowPartial)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(PaymentEscrowEscrowPartial)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *PaymentEscrowEscrowPartialIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentEscrowEscrowPartialIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentEscrowEscrowPartial represents a EscrowPartial event raised by the PaymentEscrow contract.
type PaymentEscrowEscrowPartial struct {
	Id             *big.Int
	Merchant       common.Address
	MerchantAmount *big.Int
	BuyerAmount    *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterEscrowPartial is a free log retrieval operation binding the contract event 0x8e24648d2f5009cbdbecb454ae1fb271ae2214c02307a4541435221eec0310f6.
//
// Solidity: event EscrowPartial(uint256 indexed id, address indexed merchant, uint256 merchantAmount, uint256 buyerAmount)
func (_PaymentEscrow *PaymentEscrowFilterer) FilterEscrowPartial(opts *bind.FilterOpts, id []*big.Int, merchant []common.Address) (*PaymentEscrowEscrowPartialIterator, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.FilterLogs(opts, "EscrowPartial", idRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowEscrowPartialIterator{contract: _PaymentEscrow.contract, event: "EscrowPartial", logs: logs, sub: sub}, nil
}

// WatchEscrowPartial is a free log subscription operation binding the contract event 0x8e24648d2f5009cbdbecb454ae1fb271ae2214c02307a4541435221eec0310f6.
//
// Solidity: event EscrowPartial(uint256 indexed id, address indexed merchant, uint256 merchantAmount, uint256 buyerAmount)
func (_PaymentEscrow *PaymentEscrowFilterer) WatchEscrowPartial(opts *bind.WatchOpts, sink chan<- *PaymentEscrowEscrowPartial, id []*big.Int, merchant []common.Address) (event.Subscription, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.WatchLogs(opts, "EscrowPartial", idRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentEscrowEscrowPartial)
				if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowPartial", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseEscrowPartial is a log parse operation binding the contract event 0x8e24648d2f5009cbdbecb454ae1fb271ae2214c02307a4541435221eec0310f6.
//
// Solidity: event EscrowPartial(uint256 indexed id, address indexed merchant, uint256 merchantAmount, uint256 buyerAmount)
func (_PaymentEscrow *PaymentEscrowFilterer) ParseEscrowPartial(log types.Log) (*PaymentEscrowEscrowPartial, error) {
	event := new(PaymentEscrowEscrowPartial)
	if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowPartial", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentEscrowEscrowRefundedIterator is returned from FilterEscrowRefunded and is used to iterate over the raw logs and unpacked data for EscrowRefunded events raised by the PaymentEscrow contract.
type PaymentEscrowEscrowRefundedIterator struct {
	Event *PaymentEscrowEscrowRefunded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *PaymentEscrowEscrowRefundedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentEscrowEscrowRefunded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(PaymentEscrowEscrowRefunded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *PaymentEscrowEscrowRefundedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentEscrowEscrowRefundedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentEscrowEscrowRefunded represents a EscrowRefunded event raised by the PaymentEscrow contract.
type PaymentEscrowEscrowRefunded struct {
	Id     *big.Int
	Buyer  common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterEscrowRefunded is a free log retrieval operation binding the contract event 0xeac97bc1917fcedc984e3d0671d4e83b359890323d5d1c2de32b28d17c356ced.
//
// Solidity: event EscrowRefunded(uint256 indexed id, address indexed buyer, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) FilterEscrowRefunded(opts *bind.FilterOpts, id []*big.Int, buyer []common.Address) (*PaymentEscrowEscrowRefundedIterator, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var buyerRule []interface{}
	for _, buyerItem := range buyer {
		buyerRule = append(buyerRule, buyerItem)
	}

	logs, sub, err := _PaymentEscrow.contract.FilterLogs(opts, "EscrowRefunded", idRule, buyerRule)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowEscrowRefundedIterator{contract: _PaymentEscrow.contract, event: "EscrowRefunded", logs: logs, sub: sub}, nil
}

// WatchEscrowRefunded is a free log subscription operation binding the contract event 0xeac97bc1917fcedc984e3d0671d4e83b359890323d5d1c2de32b28d17c356ced.
//
// Solidity: event EscrowRefunded(uint256 indexed id, address indexed buyer, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) WatchEscrowRefunded(opts *bind.WatchOpts, sink chan<- *PaymentEscrowEscrowRefunded, id []*big.Int, buyer []common.Address) (event.Subscription, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var buyerRule []interface{}
	for _, buyerItem := range buyer {
		buyerRule = append(buyerRule, buyerItem)
	}

	logs, sub, err := _PaymentEscrow.contract.WatchLogs(opts, "EscrowRefunded", idRule, buyerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentEscrowEscrowRefunded)
				if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowRefunded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseEscrowRefunded is a log parse operation binding the contract event 0xeac97bc1917fcedc984e3d0671d4e83b359890323d5d1c2de32b28d17c356ced.
//
// Solidity: event EscrowRefunded(uint256 indexed id, address indexed buyer, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) ParseEscrowRefunded(log types.Log) (*PaymentEscrowEscrowRefunded, error) {
	event := new(PaymentEscrowEscrowRefunded)
	if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowRefunded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentEscrowEscrowReleasedIterator is returned from FilterEscrowReleased and is used to iterate over the raw logs and unpacked data for EscrowReleased events raised by the PaymentEscrow contract.
type PaymentEscrowEscrowReleasedIterator struct {
	Event *PaymentEscrowEscrowReleased // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *PaymentEscrowEscrowReleasedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentEscrowEscrowReleased)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(PaymentEscrowEscrowReleased)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *PaymentEscrowEscrowReleasedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentEscrowEscrowReleasedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentEscrowEscrowReleased represents a EscrowReleased event raised by the PaymentEscrow contract.
type PaymentEscrowEscrowReleased struct {
	Id       *big.Int
	Merchant common.Address
	Amount   *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterEscrowReleased is a free log retrieval operation binding the contract event 0x6244ed823ca6be0f11bc890c3fafcf3c29cb23420c14243642e930b5e07e6d0a.
//
// Solidity: event EscrowReleased(uint256 indexed id, address indexed merchant, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) FilterEscrowReleased(opts *bind.FilterOpts, id []*big.Int, merchant []common.Address) (*PaymentEscrowEscrowReleasedIterator, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.FilterLogs(opts, "EscrowReleased", idRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return &PaymentEscrowEscrowReleasedIterator{contract: _PaymentEscrow.contract, event: "EscrowReleased", logs: logs, sub: sub}, nil
}

// WatchEscrowReleased is a free log subscription operation binding the contract event 0x6244ed823ca6be0f11bc890c3fafcf3c29cb23420c14243642e930b5e07e6d0a.
//
// Solidity: event EscrowReleased(uint256 indexed id, address indexed merchant, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) WatchEscrowReleased(opts *bind.WatchOpts, sink chan<- *PaymentEscrowEscrowReleased, id []*big.Int, merchant []common.Address) (event.Subscription, error) {

	var idRule []interface{}
	for _, idItem := range id {
		idRule = append(idRule, idItem)
	}
	var merchantRule []interface{}
	for _, merchantItem := range merchant {
		merchantRule = append(merchantRule, merchantItem)
	}

	logs, sub, err := _PaymentEscrow.contract.WatchLogs(opts, "EscrowReleased", idRule, merchantRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentEscrowEscrowReleased)
				if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowReleased", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseEscrowReleased is a log parse operation binding the contract event 0x6244ed823ca6be0f11bc890c3fafcf3c29cb23420c14243642e930b5e07e6d0a.
//
// Solidity: event EscrowReleased(uint256 indexed id, address indexed merchant, uint256 amount)
func (_PaymentEscrow *PaymentEscrowFilterer) ParseEscrowReleased(log types.Log) (*PaymentEscrowEscrowReleased, error) {
	event := new(PaymentEscrowEscrowReleased)
	if err := _PaymentEscrow.contract.UnpackLog(event, "EscrowReleased", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
