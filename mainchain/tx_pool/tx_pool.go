/*
 *  Copyright 2018 KardiaChain
 *  This file is part of the go-kardia library.
 *
 *  The go-kardia library is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU Lesser General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  The go-kardia library is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 *  GNU Lesser General Public License for more details.
 *
 *  You should have received a copy of the GNU Lesser General Public License
 *  along with the go-kardia library. If not, see <http://www.gnu.org/licenses/>.
 */

package tx_pool

import (
	"errors"
	"fmt"
	"github.com/kardiachain/go-kardia/kai/chaindb"
	"math/big"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/metrics"

	"github.com/kardiachain/go-kardia/configs"
	"github.com/kardiachain/go-kardia/kai/events"
	"github.com/kardiachain/go-kardia/kai/state"
	kaidb "github.com/kardiachain/go-kardia/kai/storage"
	"github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/lib/event"
	"github.com/kardiachain/go-kardia/lib/log"
	"github.com/kardiachain/go-kardia/types"
)

const (
	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10
)

var (
	// ErrInvalidSender is returned if the transaction contains an invalid signature.
	ErrInvalidSender = errors.New("invalid sender")

	// ErrNonceTooLow is returned if the nonce of a transaction is lower than the
	// one present in the local chain.
	ErrNonceTooLow = errors.New("nonce too low")

	// ErrUnderpriced is returned if a transaction's gas price is below the minimum
	// configured for the transaction pool.
	ErrUnderpriced = errors.New("transaction underpriced")

	// ErrReplaceUnderpriced is returned if a transaction is attempted to be replaced
	// with a different one without the required price bump.
	ErrReplaceUnderpriced = errors.New("replacement transaction underpriced")

	// ErrInsufficientFunds is returned if the total cost of executing a transaction
	// is higher than the balance of the user's account.
	ErrInsufficientFunds = errors.New("insufficient funds for gas * price + value")

	// ErrIntrinsicGas is returned if the transaction is specified to use less gas
	// than required to start the invocation.
	ErrIntrinsicGas = errors.New("intrinsic gas too low")

	// ErrGasLimit is returned if a transaction's requested gas limit exceeds the
	// maximum allowance of the current block.
	ErrGasLimit = errors.New("exceeds block gas limit")

	// ErrNegativeValue is a sanity error to ensure noone is able to specify a
	// transaction with a negative value.
	ErrNegativeValue = errors.New("negative value")

	// ErrOversizedData is returned if the input data of a transaction is greater
	// than some meaningful limit a user might use. This is not a consensus error
	// making the transaction invalid, rather a DOS protection.
	ErrOversizedData = errors.New("oversized data")
)

var (
	evictionInterval    = time.Minute     // Time interval to check for evictable transactions
	statsReportInterval = 8 * time.Second // Time interval to report transaction pool stats
)

var (
	// Metrics for the pending pool
	pendingDiscardCounter   = metrics.NewRegisteredCounter("txpool/pending/discard", nil)
	pendingReplaceCounter   = metrics.NewRegisteredCounter("txpool/pending/replace", nil)
	pendingRateLimitCounter = metrics.NewRegisteredCounter("txpool/pending/ratelimit", nil) // Dropped due to rate limiting
	pendingNofundsCounter   = metrics.NewRegisteredCounter("txpool/pending/nofunds", nil)   // Dropped due to out-of-funds

	// Metrics for the queued pool
	queuedDiscardCounter   = metrics.NewRegisteredCounter("txpool/queued/discard", nil)
	queuedReplaceCounter   = metrics.NewRegisteredCounter("txpool/queued/replace", nil)
	queuedRateLimitCounter = metrics.NewRegisteredCounter("txpool/queued/ratelimit", nil) // Dropped due to rate limiting
	queuedNofundsCounter   = metrics.NewRegisteredCounter("txpool/queued/nofunds", nil)   // Dropped due to out-of-funds

	// General tx metrics
	invalidTxCounter     = metrics.NewRegisteredCounter("txpool/invalid", nil)
	underpricedTxCounter = metrics.NewRegisteredCounter("txpool/underpriced", nil)
)

// TxStatus is the current status of a transaction as seen by the pool.
type TxStatus uint

const (
	TxStatusUnknown TxStatus = iota
	TxStatusQueued
	TxStatusPending
	TxStatusIncluded
)

// blockChain provides the state of blockchain and current gas limit to do
// some pre checks in tx pool and event subscribers.
type blockChain interface {
	CurrentBlock() *types.Block
	GetBlock(hash common.Hash, number uint64) *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)
	DB() kaidb.Database
	SubscribeChainHeadEvent(ch chan<- events.ChainHeadEvent) event.Subscription
}

// TxPoolConfig are the configuration parameters of the transaction pool.
type TxPoolConfig struct {
	NoLocals  bool          // Whether local transaction handling should be disabled
	Journal   string        // Journal of local transactions to survive node restarts
	Rejournal time.Duration // Time interval to regenerate the local transaction journal

	PriceLimit uint64 // Minimum gas price to enforce for acceptance into the pool
	PriceBump  uint64 // Minimum price bump percentage to replace an already existing transaction (nonce)

	AccountSlots uint64 // Minimum number of executable transaction slots guaranteed per account
	GlobalSlots  uint64 // Maximum number of executable transaction slots for all accounts
	AccountQueue uint64 // Maximum number of non-executable transaction slots permitted per account
	GlobalQueue  uint64 // Maximum number of non-executable transaction slots for all accounts

	Lifetime time.Duration // Maximum amount of time non-executable transaction are queued
}

// DefaultTxPoolConfig contains the default configurations for the transaction
// pool.
var DefaultTxPoolConfig = TxPoolConfig{
	Journal:   "transactions.rlp",
	Rejournal: time.Hour,

	PriceLimit:   1,
	PriceBump:    10,
	AccountSlots: 16,
	GlobalSlots:  4096,
	AccountQueue: 64,
	GlobalQueue:  1024,

	Lifetime: 3 * time.Hour,
}

// GetDefaultTxPoolConfig returns default txPoolConfig with given dir path
func GetDefaultTxPoolConfig(path string) *TxPoolConfig {
	conf := DefaultTxPoolConfig
	if len(path) > 0 {
		conf.Journal = filepath.Join(path, conf.Journal)
	}
	return &conf
}

// sanitize checks the provided user configurations and changes anything that's
// unreasonable or unworkable.
func (config *TxPoolConfig) sanitize(logger log.Logger) TxPoolConfig {
	conf := *config
	if conf.Rejournal < time.Second {
		logger.Warn("Sanitizing invalid txpool journal time", "provided", conf.Rejournal, "updated", time.Second)
		conf.Rejournal = time.Second
	}
	if conf.PriceLimit < 1 {
		logger.Warn("Sanitizing invalid txpool price limit", "provided", conf.PriceLimit, "updated", DefaultTxPoolConfig.PriceLimit)
		conf.PriceLimit = DefaultTxPoolConfig.PriceLimit
	}
	if conf.PriceBump < 1 {
		logger.Warn("Sanitizing invalid txpool price bump", "provided", conf.PriceBump, "updated", DefaultTxPoolConfig.PriceBump)
		conf.PriceBump = DefaultTxPoolConfig.PriceBump
	}
	return conf
}

// TxPool contains all currently known transactions. Transactions
// enter the pool when they are received from the network or submitted
// locally. They exit the pool when they are included in the blockchain.
//
// The pool separates processable transactions (which can be applied to the
// current state) and future transactions. Transactions move between those
// two states over time as they are received and processed.
type TxPool struct {
	logger log.Logger

	config       TxPoolConfig
	chainconfig  *configs.ChainConfig
	chain        blockChain
	gasPrice     *big.Int
	txFeed       event.Feed
	scope        event.SubscriptionScope
	chainHeadCh  chan events.ChainHeadEvent
	chainHeadSub event.Subscription
	mu           sync.RWMutex

	currentState *state.StateDB      // Current state in the blockchain head
	pendingState *state.ManagedState // Pending state tracking virtual nonces

	currentMaxGas uint64 // Current gas limit for transaction caps

	locals  *accountSet // Set of local transaction to exempt from eviction rules
	journal *txJournal  // Journal of local transaction to back up to disk

	pending map[common.Address]types.Transactions   // All currently processable transactions
	//queue   map[common.Address]*txList   // Queued but non-processable transactions
	beats   map[common.Address]time.Time // Last heartbeat from each known account
	all     *txLookup                    // All transactions to allow lookups
	priced  *txPricedList                // All transactions sorted by price

	wg sync.WaitGroup // for shutdown sync
}

// NewTxPool creates a new transaction pool to gather, sort and filter inbound
// transactions from the network.
func NewTxPool(logger log.Logger, config TxPoolConfig, chainconfig *configs.ChainConfig, chain blockChain) *TxPool {
	// Sanitize the input to ensure no vulnerable gas prices are set
	config = (&config).sanitize(logger)

	// Create the transaction pool with its initial settings
	pool := &TxPool{
		logger:      logger,
		config:      config,
		chainconfig: chainconfig,
		chain:       chain,
		pending:     make(map[common.Address]types.Transactions),
		//queue:       make(map[common.Address]*txList),
		beats:       make(map[common.Address]time.Time),
		all:         newTxLookup(100000), // hard code 100000 that allows caching only 100000 txs
		chainHeadCh: make(chan events.ChainHeadEvent, chainHeadChanSize),
		gasPrice:    new(big.Int).SetUint64(config.PriceLimit),
	}
	pool.locals = newAccountSet()
	pool.priced = newTxPricedList(logger, pool.all)
	pool.reset(nil, chain.CurrentBlock().Header())

	if !config.NoLocals && config.Journal != "" {
		pool.journal = newTxJournal(logger, config.Journal)

		if err := pool.journal.load(pool.AddLocals); err != nil {
			logger.Warn("Failed to load transaction journal", "err", err)
		}
		//if err := pool.journal.rotate(pool.local()); err != nil {
		//	logger.Warn("Failed to rotate transaction journal", "err", err)
		//}
	}

	// Subscribe events from blockchain
	pool.chainHeadSub = pool.chain.SubscribeChainHeadEvent(pool.chainHeadCh)

	// Start the event loop and return
	pool.wg.Add(1)
	go pool.loop()

	return pool
}

// loop is the transaction pool's main event loop, waiting for and reacting to
// outside blockchain events as well as for various reporting and transaction
// eviction events.
func (pool *TxPool) loop() {
	defer pool.wg.Done()

	// Start the stats reporting and transaction eviction tickers
	//@huny var prevPending, prevQueued, prevStales int

	/*@huny
	report := time.NewTicker(statsReportInterval)
	defer report.Stop()
	*/

	evict := time.NewTicker(evictionInterval)
	defer evict.Stop()

	journal := time.NewTicker(pool.config.Rejournal)
	defer journal.Stop()

	// Track the previous head headers for transaction reorgs
	head := pool.chain.CurrentBlock()

	// Keep waiting for and reacting to the various events
	for {
		select {
		// Handle ChainHeadEvent
		case ev := <-pool.chainHeadCh:
			if ev.Block != nil {
				pool.reset(head.Header(), ev.Block.Header())
				pool.mu.Lock()
				head = ev.Block
				pool.mu.Unlock()
			}
		// Be unsubscribed due to system stopped
		case <-pool.chainHeadSub.Err():
			return

			/*@huny
			// Handle stats reporting ticks
			case <-report.C:
				pool.mu.RLock()
				pending, queued := pool.stats()
				stales := pool.priced.stales
				pool.mu.RUnlock()

				if pending != prevPending || queued != prevQueued || stales != prevStales {
					pool.logger.Debug("Transaction pool status report", "executable", pending, "queued", queued, "stales", stales)
					prevPending, prevQueued, prevStales = pending, queued, stales
				}
			*/

		// Handle inactive account transaction eviction
		//case <-evict.C:
			//pool.mu.Lock()
			//for addr := range pool.queue {
			//	// Skip local transactions from the eviction mechanism
			//	if pool.locals.contains(addr) {
			//		continue
			//	}
			//	// Any non-locals old enough should be removed
			//	if time.Since(pool.beats[addr]) > pool.config.Lifetime {
			//		for _, tx := range pool.queue[addr].Flatten() {
			//			pool.removeTxInternal(tx.Hash(), true)
			//		}
			//	}
			//}
			//pool.mu.Unlock()

		// Handle local transaction journal rotation
		//case <-journal.C:
			//if pool.journal != nil {
			//	pool.mu.Lock()
			//	if err := pool.journal.rotate(pool.local()); err != nil {
			//		pool.logger.Warn("Failed to rotate local tx journal", "err", err)
			//	}
			//	pool.mu.Unlock()
			//}
		}
	}
}

// lockedReset is a wrapper around reset to allow calling it in a thread safe
// manner. This method is only ever used in the tester!
func (pool *TxPool) lockedReset(oldHead, newHead *types.Header) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.reset(oldHead, newHead)
}

// reset retrieves the current state of the blockchain and ensures the content
// of the transaction pool is valid with regard to the chain state.
func (pool *TxPool) reset(oldHead, newHead *types.Header) {
	// Note: Disables feature of recreate dropped transaction, will evaluate this for mainnet.
	/*
		// If we're reorging an old state, reinject all dropped transactions
		var reinject types.Transactions

			if oldHead != nil && oldHead.Hash() != newHead.LastCommitHash {
				// If the reorg is too deep, avoid doing it (will happen during fast sync)
				oldHeight := oldHead.Height
				newHeight := newHead.Height

				if depth := uint64(math.Abs(float64(oldHeight) - float64(newHeight))); depth > 64 {
					pool.logger.Debug("Skipping deep transaction reorg", "depth", depth)
				} else {
					// Reorg seems shallow enough to pull in all transactions into memory
					var discarded, included types.Transactions

					var (
						rem = pool.chain.GetBlock(oldHead.Hash(), oldHead.Height)
						add = pool.chain.GetBlock(newHead.Hash(), newHead.Height)
					)
					for rem.Height() > add.Height() {
						discarded = append(discarded, rem.Transactions()...)
						if rem = pool.chain.GetBlock(rem.LastCommitHash(), rem.Height()-1); rem == nil {
							pool.logger.Error("Unrooted old chain seen by tx pool", "block", oldHead.Height, "hash", oldHead.Hash())
							return
						}
					}
					for add.Height() > rem.Height() {
						included = append(included, add.Transactions()...)
						if add = pool.chain.GetBlock(add.LastCommitHash(), add.Height()-1); add == nil {
							pool.logger.Error("Unrooted new chain seen by tx pool", "block", newHead.Height, "hash", newHead.Hash())
							return
						}
					}
					for rem.Hash() != add.Hash() {
						discarded = append(discarded, rem.Transactions()...)
						if rem = pool.chain.GetBlock(rem.LastCommitHash(), rem.Height()-1); rem == nil {
							pool.logger.Error("Unrooted old chain seen by tx pool", "block", oldHead.Height, "hash", oldHead.Hash())
							return
						}
						included = append(included, add.Transactions()...)
						if add = pool.chain.GetBlock(add.LastCommitHash(), add.Height()-1); add == nil {
							pool.logger.Error("Unrooted new chain seen by tx pool", "block", newHead.Height, "hash", newHead.Hash())
							return
						}
					}
					reinject = types.TxDifference(discarded, included)
				}
			}
	*/

	// Initialize the internal state to the current head
	pool.mu.Lock()
	if newHead == nil {
		newHead = pool.chain.CurrentBlock().Header() // Special case during testing
	}

	statedb, err := pool.chain.StateAt(newHead.Root)
	pool.logger.Info("TxPool reset state to new head block", "height", newHead.Height, "root", newHead.Root)
	if err != nil {
		pool.logger.Error("Failed to reset txpool state", "err", err)
		return
	}

	pool.currentState = statedb
	pool.pendingState = state.ManageState(statedb)

	pool.currentMaxGas = newHead.GasLimit

	pool.mu.Unlock()
	// get pending list in order to sort and update pending state
	if _, _, err := pool.Pending(0); err != nil {
		pool.logger.Error("reset - error while getting pending list", "err", err)
	}

	// Inject any transactions discarded due to reorgs
	//pool.logger.Debug("Reinjecting stale transactions", "count", len(reinject))
	//senderCacher.recover(reinject)
	//pool.addTxsLocked(reinject, false)

	// validate the pool of pending transactions, this will remove
	// any transactions that have been included in the block or
	// have been invalidated because of another transaction (e.g.
	// higher gas price)
	//pool.demoteUnexecutables()

	// Update all accounts to the latest known pending nonce
	//for addr, list := range pool.pending {
	//	txs := list.Flatten() // Heavy but will be cached and is needed by the miner anyway
	//	pool.pendingState.SetNonce(addr, txs[len(txs)-1].Nonce()+1)
	//}

	// Check the queue and move transactions over to the pending if possible
	// or remove those that have become invalid
	//pool.promoteExecutables(nil)
}

// Stop terminates the transaction pool.
func (pool *TxPool) Stop() {
	// Unsubscribe all subscriptions registered from txpool
	pool.scope.Close()

	// Unsubscribe subscriptions registered from blockchain
	pool.chainHeadSub.Unsubscribe()
	pool.wg.Wait()

	if pool.journal != nil {
		pool.journal.close()
	}
	pool.logger.Info("Transaction pool stopped")
}

// SubscribeNewTxsEvent registers a subscription of NewTxsEvent and
// starts sending event to the given channel.
func (pool *TxPool) SubscribeNewTxsEvent(ch chan<- events.NewTxsEvent) event.Subscription {
	return pool.scope.Track(pool.txFeed.Subscribe(ch))
}

// GasPrice returns the current gas price enforced by the transaction pool.
func (pool *TxPool) GasPrice() *big.Int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return new(big.Int).Set(pool.gasPrice)
}

// SetGasPrice updates the minimum price required by the transaction pool for a
// new transaction, and drops all transactions below this threshold.
//func (pool *TxPool) SetGasPrice(price *big.Int) {
//	pool.mu.Lock()
//	defer pool.mu.Unlock()
//
//	pool.gasPrice = price
//	for _, tx := range pool.priced.Cap(price, pool.locals) {
//		pool.removeTxInternal(tx.Hash(), false)
//	}
//	pool.logger.Info("Transaction pool price threshold updated", "price", price)
//}

// State returns the virtual managed state of the transaction pool.
func (pool *TxPool) State() *state.ManagedState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.pendingState
}

func (pool *TxPool) CurrentState() *state.StateDB {
	return pool.currentState
}

/*
// Stats retrieves the current pool stats, namely the number of pending and the
// number of queued (non-executable) transactions.
func (pool *TxPool) Stats() (int, int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.stats()
}

// stats retrieves the current pool stats, namely the number of pending and the
// number of queued (non-executable) transactions.
func (pool *TxPool) stats() (int, int) {
	pending := 0
	for _, list := range pool.pending {
		pending += list.Len()
	}
	queued := 0
	for _, list := range pool.queue {
		queued += list.Len()
	}
	return pending, queued
}
*/

func (pool *TxPool) pendingValidation(tx *types.Transaction) error {

	from, err := types.Sender(tx)
	if err != nil {
		return ErrInvalidSender
	}

	// Ensure the transaction adheres to nonce ordering
	if pool.currentState.GetNonce(from) > tx.Nonce() {
		return ErrNonceTooLow
	}

	// Transactor should have enough funds to cover the costs
	// cost == V + GP * GL
	if pool.currentState.GetBalance(from).Cmp(tx.Cost()) < 0 {
		pool.logger.Error("Bad txn cost", "balance", pool.currentState.GetBalance(from), "cost", tx.Cost(), "from", from)
		return ErrInsufficientFunds
	}

	if t, _, _, _ := chaindb.ReadTransaction(pool.chain.DB(), tx.Hash()); t != nil {
		return fmt.Errorf("known transaction: %x", tx.Hash())
	}

	return nil
}

func removeTxsFromSlices(txs types.Transactions, indexes []int) types.Transactions {
	if len(indexes) == 0 || len(txs) == 0 {return nil}
	sort.Ints(indexes)
	newTxs := make(types.Transactions, 0)
	if len(indexes) == 1 {
		if indexes[0] > len(txs) {
			return newTxs
		}
		if indexes[0] == 0 {
			newTxs = append(newTxs, txs[1:]...)
		} else if indexes[0] == len(txs)-1 {
			newTxs = append(newTxs, txs[0:len(txs)-1]...)
		} else {
			newTxs = append(newTxs, txs[0:indexes[0]]...)
			if indexes[0] + 1 < len(txs)-1 {
				newTxs = append(newTxs, txs[indexes[0]+1:]...)
			} else if indexes[0] + 1 < len(txs) {
				newTxs = append(newTxs, txs[indexes[0]+1])
			}
		}
	} else {
		for i, idx := range indexes {
			if idx < len(txs) - 1 {
				if i < len(indexes) - 1 {
					if idx + 1 == indexes[i+1] || idx == indexes[i+1] {
						continue
					}
					//log.Error("removeTxsFromSlices", "idx+1", idx+1, "indexes", indexes, "i", i, "tx", len(txs))
					// sometime idx+1 can be greater than len of txs
					if indexes[i+1] < len(txs) {
						newTxs = append(newTxs, txs[idx+1:indexes[i+1]]...)
					} else {
						newTxs = append(newTxs, txs[idx+1:]...)
					}

				} else {
					newTxs = append(newTxs, txs[idx+1:]...)
				}
			}
		}
	}
	return newTxs
}

func (pool *TxPool) RemovePending(limit int) types.Transactions {
	txs, indexes, _ := pool.Pending(limit)
	// remove indexes
	pool.mu.Lock()
	for addr, idx := range indexes {
		//pool.logger.Error("Remove Pending", "addr", addr.Hex(), "indexes", idx, "txs", len(*pool.pending[addr]))
		pool.pending[addr] = removeTxsFromSlices(pool.pending[addr], idx)
	}
	pool.mu.Unlock()
	return txs
}

func (pool *TxPool) RemoveTxsFromPending(txs types.Transactions) error {
	pool.mu.Lock()

	txLoop:
	for _, tx := range txs {
		addr, err := types.Sender(tx)
		if err != nil {
			pool.mu.Unlock()
			return err
		}

		if _, ok := pool.pending[addr]; ok {
			for i, pendingTx := range pool.pending[addr] {
				if pendingTx.Hash() == tx.Hash() {
					removedIndexes := make([]int, 0)
					removedIndexes = append(removedIndexes, i)
					pool.pending[addr] = removeTxsFromSlices(pool.pending[addr], removedIndexes)
					continue txLoop
				}
			}
		}
	}
	pool.mu.Unlock()
	return nil
}

// Pending retrieves all currently processable transactions, groupped by origin
// account and sorted by nonce. The returned transaction set is a copy and can be
// freely modified by calling code.
func (pool *TxPool) Pending(limit int) (types.Transactions, map[common.Address][]int, error) {
	pending := make(types.Transactions, 0)
	pool.mu.Lock()
	// indexes is found txs indexes in pool.pending
	indexes := make(map[common.Address][]int, 0)
	for addr, pendingTxs := range pool.pending {

		if pendingTxs == nil {
			continue
		}

		// removedIndexes store all invalid txs indexes that will be deleted after loop
		removedIndexes := make([]int, 0)

		// txs is a list of valid txs, txs will be sorted after loop
		txs := make(types.Transactions, 0)

		// nonces is a map that key is nonce, it is used to prevent overlap transactions
		nonces := make(map[uint64]bool)
		indexes[addr] = make([]int, 0)

		for i, tx := range pendingTxs {

			if limit > 0 && len(pending) + len(txs) >= limit {
				break
			}

			if _, ok := nonces[tx.Nonce()]; ok {
				removedIndexes = append(removedIndexes, i)
				continue
			}
			if err := pool.pendingValidation(tx); err != nil {
				// add i into removedIndexes
				removedIndexes = append(removedIndexes, i)

				// remove tx from all and priced
				pool.all.Remove(tx.Hash())
				pool.priced.Removed()
				continue
			}
			nonces[tx.Nonce()] = true
			txs = append(txs, tx)
			indexes[addr] = append(indexes[addr], i)
		}

		if len(removedIndexes) > 0 {
			pool.pending[addr] = removeTxsFromSlices(pendingTxs, removedIndexes)
		}

		if len(txs) > 0 {
			sort.Sort(types.TxByNonce(txs))
			pending = append(pending, txs...)

			// update pending state for address
			pool.pendingState.SetNonce(addr, txs[len(txs)-1].Nonce()+1)
		}
	}
	pool.mu.Unlock()
	return pending, indexes, nil
}

// local retrieves all currently known local transactions, groupped by origin
// account and sorted by nonce. The returned transaction set is a copy and can be
// freely modified by calling code.
//func (pool *TxPool) local() map[common.Address]types.Transactions {
//	txs := make(map[common.Address]types.Transactions)
//	for addr := range pool.locals.accounts {
//		if pending := pool.pending[addr]; pending != nil {
//			txs[addr] = append(txs[addr], pending.Flatten()...)
//		}
//		//if queued := pool.queue[addr]; queued != nil {
//		//	txs[addr] = append(txs[addr], queued.Flatten()...)
//		//}
//	}
//	return txs
//}

// validateTx checks whether a transaction is valid according to the consensus
// rules and adheres to some heuristic limits of the local node (price and size).
func (pool *TxPool) validateTx(tx *types.Transaction/*, local bool*/) error {
	// Heuristic limit, reject transactions over 32KB to prevent DOS attacks
	if tx.Size() > 32*1024 {
		return ErrOversizedData
	}
	// Transactions can't be negative. This may never happen using RLP decoded
	// transactions but may occur if you create a transaction using the RPC.
	if tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}
	// Ensure the transaction doesn't exceed the current block limit gas.
	if pool.currentMaxGas < tx.Gas() {
		return ErrGasLimit
	}
	// Make sure the transaction is signed properly
	//from, err := types.Sender(tx)
	//if err != nil {
	//	return ErrInvalidSender
	//}
	// Drop non-local transactions under our own minimal accepted gas price
	//local = local || pool.locals.contains(from) // account may be local even if the transaction arrived from the network
	if pool.gasPrice.Cmp(tx.GasPrice()) > 0 {
		return ErrUnderpriced
	}

	if pool.all.Get(tx.Hash()) != nil {
		return fmt.Errorf("known transaction %v", tx.Hash().Hex())
	}

	// Ensure the transaction adheres to nonce ordering
	//if pool.currentState.GetNonce(from) > tx.Nonce() {
	//	return ErrNonceTooLow
	//}

	// Transactor should have enough funds to cover the costs
	// cost == V + GP * GL
	//if pool.currentState.GetBalance(from).Cmp(tx.Cost()) < 0 {
	//	pool.logger.Error("Bad txn cost", "balance", pool.currentState.GetBalance(from), "cost", tx.Cost(), "from", from)
	//	return ErrInsufficientFunds
	//}

	/*@huny
	intrGas, err := IntrinsicGas(tx.Data(), tx.To() == nil, pool.homestead)
	if err != nil {
		return err
	}
	if tx.Gas() < intrGas {
		return ErrIntrinsicGas
	}
	*/
	return nil
}

// add validates a transaction and inserts it into the non-executable queue for
// later pending promotion and execution. If the transaction is a replacement for
// an already pending or queued one, it overwrites the previous and returns this
// so outer code doesn't uselessly call promote.
//
// If a newly added transaction is marked as local, its sending account will be
// whitelisted, preventing any associated transaction from being dropped out of
// the pool due to pricing constraints.
func (pool *TxPool) add(tx *types.Transaction, local bool) (bool, error) {
	// If the transaction is already known, discard it
	//hash := tx.Hash()

	// If transaction exists in pool or DB, discard it
	//if t, _, _, _ := chaindb.ReadTransaction(pool.chain.DB(), hash); pool.all.Get(hash) != nil || t != nil {
	//	pool.logger.Trace("Discarding already known transaction", "hash", hash)
	//	return false, fmt.Errorf("known transaction: %x", hash)
	//}
	// If the transaction fails basic validation, discard it
	if err := pool.validateTx(tx/*, local*/); err != nil {
		//pool.logger.Trace("Discarding invalid transaction", "hash", tx.Hash().Hex(), "err", err)
		//invalidTxCounter.Inc(1)
		return false, err
	}

	return true, nil
	// If the transaction pool is full, discard underpriced transactions
	//if uint64(pool.all.Count()) >= pool.config.GlobalSlots+pool.config.GlobalQueue {
	//	// If the new transaction is underpriced, don't accept it
	//	if !local && pool.priced.Underpriced(tx, pool.locals) {
	//		pool.logger.Trace("Discarding underpriced transaction", "hash", hash, "price", tx.GasPrice())
	//		underpricedTxCounter.Inc(1)
	//		return false, ErrUnderpriced
	//	}
	//	// New transaction is better than our worse ones, make room for it
	//	drop := pool.priced.Discard(pool.all.Count()-int(pool.config.GlobalSlots+pool.config.GlobalQueue-1), pool.locals)
	//	for _, tx := range drop {
	//		pool.logger.Trace("Discarding freshly underpriced transaction", "hash", tx.Hash(), "price", tx.GasPrice())
	//		underpricedTxCounter.Inc(1)
	//		pool.removeTxInternal(tx.Hash(), false)
	//	}
	//}
	// If the transaction is replacing an already pending one, do directly
	//from, _ := types.Sender(tx) // already validated
	//if list := pool.pending[from]; list != nil && list.Overlaps(tx) {
	//	// Nonce already pending, check if required price bump is met
	//	inserted, old := list.Add(tx, pool.config.PriceBump)
	//	if !inserted {
	//		pendingDiscardCounter.Inc(1)
	//		return false, ErrReplaceUnderpriced
	//	}
	//	// New transaction is better, replace old one
	//	if old != nil {
	//		pool.all.Remove(old.Hash())
	//		pool.priced.Removed()
	//		pendingReplaceCounter.Inc(1)
	//	}
	//	pool.all.Add(tx)
	//	pool.priced.Put(tx)
	//	pool.journalTx(from, tx)
	//
	//	pool.logger.Trace("Pooled new executable transaction", "hash", hash, "from", from, "to", tx.To())
	//
	//	// We've directly injected a replacement transaction, notify subsystems
	//	go pool.txFeed.Send(events.NewTxsEvent{types.Transactions{tx}})
	//
	//	return old != nil, nil
	//}
	// New transaction isn't replacing a pending one, push into queue
	//replace, err := pool.enqueueTx(hash, tx)
	//if err != nil {
	//	return false, err
	//}
	//
	//// Mark local addresses and journal local transactions
	//if local {
	//	pool.locals.add(from)
	//}
	//pool.journalTx(from, tx)
	//
	//pool.logger.Trace("Pooled new future transaction", "hash", hash, "from", from, "to", tx.To())
	//return replace, nil
}

// enqueueTx inserts a new transaction into the non-executable transaction queue.
//
// Note, this method assumes the pool lock is held!
//func (pool *TxPool) enqueueTx(hash common.Hash, tx *types.Transaction) (bool, error) {
//	// Try to insert the transaction into the future queue
//	from, _ := types.Sender(tx) // already validated
//	if pool.queue[from] == nil {
//		pool.queue[from] = newTxList(false)
//	}
//	inserted, old := pool.queue[from].Add(tx, pool.config.PriceBump)
//	if !inserted {
//		// An older transaction was better, discard this
//		queuedDiscardCounter.Inc(1)
//		return false, ErrReplaceUnderpriced
//	}
//	// Discard any previous transaction and mark this
//	if old != nil {
//		pool.all.Remove(old.Hash())
//		pool.priced.Removed()
//		queuedReplaceCounter.Inc(1)
//	}
//	if pool.all.Get(hash) == nil {
//		pool.all.Add(tx)
//		pool.priced.Put(tx)
//	}
//	return old != nil, nil
//}

// journalTx adds the specified transaction to the local disk journal if it is
// deemed to have been sent from a local account.
//func (pool *TxPool) journalTx(from common.Address, tx *types.Transaction) {
//	// Only journal if it's enabled and the transaction is local
//	if pool.journal == nil || !pool.locals.contains(from) {
//		return
//	}
//	if err := pool.journal.insert(tx); err != nil {
//		pool.logger.Warn("Failed to journal local transaction", "err", err)
//	}
//}

// promoteTx adds a transaction to the pending (processable) list of transactions
// and returns whether it was inserted or an older was better.
//
// Note, this method assumes the pool lock is held!
//func (pool *TxPool) promoteTx(addr common.Address, hash common.Hash, tx *types.Transaction) bool {
//	// Try to insert the transaction into the pending queue
//	txs := make(types.Transactions, 0)
//	if pool.pending[addr] != nil {
//		txs = *pool.pending[addr]
//	}
//	txs = append(txs, tx)
//	pool.pending[addr] = &txs
//	//inserted, old := list.Add(tx, pool.config.PriceBump)
//	//if !inserted {
//	//	// An older transaction was better, discard this
//	//	pool.all.Remove(hash)
//	//	pool.priced.Removed()
//	//
//	//	pendingDiscardCounter.Inc(1)
//	//	return false
//	//}
//	//// Otherwise discard any previous transaction and mark this
//	//if old != nil {
//	//	pool.all.Remove(old.Hash())
//	//	pool.priced.Removed()
//	//
//	//	pendingReplaceCounter.Inc(1)
//	//}
//	//// Failsafe to work around direct pending inserts (tests)
//	if pool.all.Get(hash) == nil {
//		pool.all.Add(tx)
//		pool.priced.Put(tx)
//	}
//	//// Set the potentially new pending nonce and notify any subsystems of the new tx
//	//pool.beats[addr] = time.Now()
//	//pool.pendingState.SetNonce(addr, tx.Nonce()+1)
//
//	return true
//}

// AddLocal enqueues a single transaction into the pool if it is valid, marking
// the sender as a local one in the mean time, ensuring it goes around the local
// pricing constraints.
func (pool *TxPool) AddLocal(tx *types.Transaction) error {
	if err := pool.addTx(tx, !pool.config.NoLocals); err != nil {
		return err
	}
	//go pool.txFeed.Send(events.NewTxsEvent{Txs: []*types.Transaction{tx}})
	return nil
}

// AddRemote enqueues a single transaction into the pool if it is valid. If the
// sender is not among the locally tracked ones, full pricing constraints will
// apply.
func (pool *TxPool) AddRemote(tx *types.Transaction) error {
	if err := pool.addTx(tx, false); err != nil {
		return err
	}
	//go pool.txFeed.Send(events.NewTxsEvent{Txs: []*types.Transaction{tx}})
	return nil
}

// AddLocals enqueues a batch of transactions into the pool if they are valid,
// marking the senders as a local ones in the mean time, ensuring they go around
// the local pricing constraints.
func (pool *TxPool) AddLocals(txs []*types.Transaction) []error {
	return pool.addTxs(txs, !pool.config.NoLocals)
}

// AddRemotes enqueues a batch of transactions into the pool if they are valid.
// If the senders are not among the locally tracked ones, full pricing constraints
// will apply.
func (pool *TxPool) AddRemotes(txs []*types.Transaction) []error {
	return pool.addTxs(txs, false)
}

// addTx enqueues a single transaction into the pool if it is valid.
func (pool *TxPool) addTx(tx *types.Transaction, local bool) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	// Try to inject the transaction and update any state
	if err := pool.validateTx(tx/*, local*/); err != nil {
		pool.logger.Trace("Discarding invalid transaction", "hash", tx.Hash().Hex(), "err", err)
		invalidTxCounter.Inc(1)
		return err
	}

	txs := make(types.Transactions, 0)
	from, _ := types.Sender(tx)
	if pool.pending[from] != nil {
		txs = pool.pending[from]
	}

	txs = append(txs, tx)
	pool.pending[from] = txs

	// add tx to all
	if pool.all.Get(tx.Hash()) == nil {
		pool.all.Add(tx)
		pool.priced.Put(tx)
	}

	//replace, err := pool.add(tx, local)
	//if err != nil {
	//	return err
	//}
	// If we added a new transaction, run promotion checks and return
	//if !replace {
	//from, _ := types.Sender(tx) // already validated
	//pool.promoteExecutables([]common.Address{from})
	//}
	return nil
}

// addTxs attempts to queue a batch of transactions if they are valid.
func (pool *TxPool) addTxs(txs []*types.Transaction, local bool) []error {
	//pool.mu.Lock()
	//defer pool.mu.Unlock()

	errs := make([]error, len(txs))
	promoted := make([]*types.Transaction, 0)

	for i, tx := range txs {
		if tx == nil {
			continue
		}
		errs[i] = pool.addTx(tx, local)
		if errs[i] == nil {
			promoted = append(promoted, tx)
		}
	}

	if len(promoted) > 0 {
		go pool.txFeed.Send(events.NewTxsEvent{Txs: promoted})
	}

	return errs
}

// addTxsLocked attempts to queue a batch of transactions if they are valid,
// whilst assuming the transaction pool lock is already held.
//func (pool *TxPool) addTxsLocked(txs []*types.Transaction, local bool) []error {
//	// Add the batch of transaction, tracking the accepted ones
//	dirty := make(map[common.Address]struct{})
//	errs := make([]error, len(txs))
//
//	for i, tx := range txs {
//		var replace bool
//		if replace, errs[i] = pool.add(tx, local); errs[i] == nil && !replace {
//			from, _ := types.Sender(tx) // already validated
//			dirty[from] = struct{}{}
//		}
//	}
//	// Only reprocess the internal state if something was actually added
//	if len(dirty) > 0 {
//		addrs := make([]common.Address, 0, len(dirty))
//		for addr := range dirty {
//			addrs = append(addrs, addr)
//		}
//		pool.promoteExecutables(addrs)
//	}
//	return errs
//}

// Status returns the status (unknown/pending/queued) of a batch of transactions
// identified by their hashes.
//func (pool *TxPool) Status(hashes []common.Hash) []TxStatus {
//	pool.mu.RLock()
//	defer pool.mu.RUnlock()
//
//	status := make([]TxStatus, len(hashes))
//	for i, hash := range hashes {
//		if tx := pool.all.Get(hash); tx != nil {
//			from, _ := types.Sender(tx) // already validated
//			if pool.pending[from] != nil && pool.pending[from].txs.items[tx.Nonce()] != nil {
//				status[i] = TxStatusPending
//			} else {
//				status[i] = TxStatusQueued
//			}
//		}
//	}
//	return status
//}

// Get returns a transaction if it is contained in the pool
// and nil otherwise.
//func (pool *TxPool) Get(hash common.Hash) *types.Transaction {
//	return pool.all.Get(hash)
//}

// RemoveTx removes transactions from pending queue.
// This function is mainly for caller in blockchain/consensus to directly remove committed txs.
//
func (pool *TxPool) RemoveTxs(txs types.Transactions) {
	if err := pool.RemoveTxsFromPending(txs); err != nil {
		pool.logger.Error("error while trying remove pending Txs", "err", err)
	}
	//pool.mu.Lock()
	//for _, tx := range txs {
	//	pool.all.Remove(tx.Hash())
	//	pool.priced.Removed()
	//}
	//pool.mu.Unlock()
}

// removeTxInternal removes a single transaction from the queue, moving all subsequent
// transactions back to the future queue. Pool pendingState is also reset.
// Caller is assumed to hold pool.mu.Lock()
//func (pool *TxPool) removeTxInternal(hash common.Hash, outofbound bool) {
//	// Fetch the transaction we wish to delete
//	tx := pool.all.Get(hash)
//	if tx == nil {
//		return
//	}
//	addr, _ := types.Sender(tx) // already validated during insertion
//
//	// Remove it from the list of known transactions
//	pool.all.Remove(hash)
//	if outofbound {
//		pool.priced.Removed()
//	}
//	// Remove the transaction from the pending lists and reset the account nonce
//	if pending := pool.pending[addr]; pending != nil {
//		if removed, invalids := pending.Remove(tx); removed {
//			// If no more pending transactions are left, remove the list
//			if pending.Empty() {
//				delete(pool.pending, addr)
//				delete(pool.beats, addr)
//			}
//			// Postpone any invalidated transactions
//			for _, tx := range invalids {
//				pool.enqueueTx(tx.Hash(), tx)
//			}
//
//			// Update the account nonce if needed
//			if nonce := tx.Nonce(); pool.pendingState.GetNonce(addr) > nonce {
//				pool.pendingState.SetNonce(addr, nonce)
//			}
//
//			return
//		}
//	}
//	// Transaction is in the future queue
//	if future := pool.queue[addr]; future != nil {
//		future.Remove(tx)
//		if future.Empty() {
//			delete(pool.queue, addr)
//		}
//	}
//}

// promoteExecutables moves transactions that have become processable from the
// future queue to the set of pending transactions. During this process, all
// invalidated transactions (low nonce, low balance) are deleted.
func (pool *TxPool) promoteExecutables(accounts []common.Address) {
	// Track the promoted transactions to broadcast them at once
	//var promoted []*types.Transaction
	//
	//// Gather all the accounts potentially needing updates
	//if accounts == nil {
	//	accounts = make([]common.Address, 0)
	//	for addr := range pool.pending {
	//		accounts = append(accounts, addr)
	//	}
	//}
	//oldTxDrop := 0
	//// Iterate over all accounts and promote any executable transactions
	//for _, addr := range accounts {
	//	list := pool.queue[addr]
	//	if list == nil {
	//		continue // Just in case someone calls with a non existing account
	//	}
	//	// Drop all transactions that are deemed too old (low nonce)
	//	for _, tx := range list.Forward(pool.currentState.GetNonce(addr)) {
	//		oldTxDrop++
	//		hash := tx.Hash()
	//		pool.all.Remove(hash)
	//		pool.priced.Removed()
	//	}
	//	/*
	//		// Drop all transactions that are too costly (low balance or out of gas)
	//		drops, _ := list.Filter(pool.currentState.GetBalance(addr), pool.currentMaxGas)
	//		for _, tx := range drops {
	//			hash := tx.Hash()
	//			pool.logger.Info("Removed unpayable queued transaction", "hash", hash)
	//			pool.all.Remove(hash)
	//			pool.priced.Removed()
	//			queuedNofundsCounter.Inc(1)
	//		}
	//	*/
	//
	//	// Gather all executable transactions and promote them
	//	for _, tx := range list.Ready(pool.pendingState.GetNonce(addr)) {
	//		hash := tx.Hash()
	//		if pool.promoteTx(addr, hash, tx) {
	//			promoted = append(promoted, tx)
	//		} else {
	//			pool.logger.Error("Fail to promote tx", "tx", tx)
	//		}
	//	}
	//
	//	// Drop all transactions over the allowed limit
	//	if !pool.locals.contains(addr) {
	//		for _, tx := range list.Cap(int(pool.config.AccountQueue)) {
	//			hash := tx.Hash()
	//			pool.all.Remove(hash)
	//			pool.priced.Removed()
	//			queuedRateLimitCounter.Inc(1)
	//			pool.logger.Error("Removed cap-exceeding queued transaction", "hash", hash)
	//		}
	//	}
	//	// Delete the entire queue entry if it became empty.
	//	if list.Empty() {
	//		delete(pool.queue, addr)
	//	}
	//}
	//if oldTxDrop > 0 {
	//	pool.logger.Info("promoteExecutables: Drop txs that are low nonce [likely committed txs]",
	//		"number of txs", oldTxDrop,
	//		"block height", pool.chain.CurrentBlock().Height())
	//}
	//
	//// Notify subsystem for new promoted transactions.
	//if len(promoted) > 0 {
	//	go pool.txFeed.Send(events.NewTxsEvent{promoted})
	//}
	//// If the pending limit is overflown, start equalizing allowances
	//pending := uint64(0)
	//for _, list := range pool.pending {
	//	pending += uint64(list.Len())
	//}
	//if pending > pool.config.GlobalSlots {
	//	pendingBeforeCap := pending
	//	// Assemble a spam order to penalize large transactors first
	//	spammers := prque.New()
	//	for addr, list := range pool.pending {
	//		// Only evict transactions from high rollers
	//		if !pool.locals.contains(addr) && uint64(list.Len()) > pool.config.AccountSlots {
	//			spammers.Push(addr, float32(list.Len()))
	//		}
	//	}
	//	// Gradually drop transactions from offenders
	//	offenders := []common.Address{}
	//	for pending > pool.config.GlobalSlots && !spammers.Empty() {
	//		// Retrieve the next offender if not local address
	//		offender, _ := spammers.Pop()
	//		offenders = append(offenders, offender.(common.Address))
	//
	//		// Equalize balances until all the same or below threshold
	//		if len(offenders) > 1 {
	//			// Calculate the equalization threshold for all current offenders
	//			threshold := pool.pending[offender.(common.Address)].Len()
	//
	//			// Iteratively reduce all offenders until below limit or threshold reached
	//			for pending > pool.config.GlobalSlots && pool.pending[offenders[len(offenders)-2]].Len() > threshold {
	//				for i := 0; i < len(offenders)-1; i++ {
	//					list := pool.pending[offenders[i]]
	//					for _, tx := range list.Cap(list.Len() - 1) {
	//						// Drop the transaction from the global pools too
	//						hash := tx.Hash()
	//						pool.all.Remove(hash)
	//						pool.priced.Removed()
	//
	//						// Update the account nonce to the dropped transaction
	//						if nonce := tx.Nonce(); pool.pendingState.GetNonce(offenders[i]) > nonce {
	//							pool.pendingState.SetNonce(offenders[i], nonce)
	//						}
	//
	//						pool.logger.Error("Removed fairness-exceeding pending transaction", "hash", hash)
	//					}
	//					pending--
	//				}
	//			}
	//		}
	//	}
	//	// If still above threshold, reduce to limit or min allowance
	//	if pending > pool.config.GlobalSlots && len(offenders) > 0 {
	//		for pending > pool.config.GlobalSlots && uint64(pool.pending[offenders[len(offenders)-1]].Len()) > pool.config.AccountSlots {
	//			for _, addr := range offenders {
	//				list := pool.pending[addr]
	//				for _, tx := range list.Cap(list.Len() - 1) {
	//					// Drop the transaction from the global pools too
	//					hash := tx.Hash()
	//					pool.all.Remove(hash)
	//					pool.priced.Removed()
	//
	//					// Update the account nonce to the dropped transaction
	//					if nonce := tx.Nonce(); pool.pendingState.GetNonce(addr) > nonce {
	//						pool.pendingState.SetNonce(addr, nonce)
	//					}
	//
	//					pool.logger.Error("Removed fairness-exceeding pending transaction", "hash", hash)
	//				}
	//				pending--
	//			}
	//		}
	//	}
	//	pendingRateLimitCounter.Inc(int64(pendingBeforeCap - pending))
	//}
	//// If we've queued more transactions than the hard limit, drop oldest ones
	//queued := uint64(0)
	//for _, list := range pool.queue {
	//	queued += uint64(list.Len())
	//}
	//if queued > pool.config.GlobalQueue {
	//	// Sort all accounts with queued transactions by heartbeat
	//	addresses := make(addresssByHeartbeat, 0, len(pool.queue))
	//	for addr := range pool.queue {
	//		if !pool.locals.contains(addr) { // don't drop locals
	//			addresses = append(addresses, addressByHeartbeat{addr, pool.beats[addr]})
	//		}
	//	}
	//	sort.Sort(addresses)
	//
	//	// Drop transactions until the total is below the limit or only locals remain
	//	for drop := queued - pool.config.GlobalQueue; drop > 0 && len(addresses) > 0; {
	//		addr := addresses[len(addresses)-1]
	//		list := pool.queue[addr.address]
	//
	//		addresses = addresses[:len(addresses)-1]
	//
	//		// Drop all transactions if they are less than the overflow
	//		if size := uint64(list.Len()); size <= drop {
	//			for _, tx := range list.Flatten() {
	//				pool.removeTxInternal(tx.Hash(), true)
	//				pool.logger.Error("Drop tx until less than the overflow", "hash", tx.Hash())
	//			}
	//			drop -= size
	//			queuedRateLimitCounter.Inc(int64(size))
	//			continue
	//		}
	//		// Otherwise drop only last few transactions
	//		txs := list.Flatten()
	//		for i := len(txs) - 1; i >= 0 && drop > 0; i-- {
	//			pool.removeTxInternal(txs[i].Hash(), true)
	//			pool.logger.Error("Drop last few txs", "hash", txs[i].Hash())
	//			drop--
	//			queuedRateLimitCounter.Inc(1)
	//		}
	//	}
	//}
}

// demoteUnexecutables removes invalid and processed transactions from the pools
// executable/pending queue and any subsequent transactions that become unexecutable
// are moved back into the future queue.
//func (pool *TxPool) demoteUnexecutables() {
//	// TODO(thientn): Evaluate this for future phases.
//	// These txs should also dropped by below loop because of low nonce.
//	// Drop transactions included in latest block, assume it's committed and saved.
//	// This function is only called when TxPool detect new height.
//	for _, tx := range pool.chain.CurrentBlock().Transactions() {
//		hash := tx.Hash()
//		pool.all.Remove(hash)
//		pool.priced.Removed()
//	}
//	pool.logger.Info("Drop committed txs from recent block",
//		"number of txs", pool.chain.CurrentBlock().NumTxs(),
//		"block height", pool.chain.CurrentBlock().Height())
//
//	oldTxDrop := 0
//	// Iterate over all accounts and demote any non-executable transactions
//	for addr, list := range pool.pending {
//		nonce := pool.currentState.GetNonce(addr)
//
//		// Drop all transactions that are deemed too old (low nonce)
//		for _, tx := range list.Forward(nonce) {
//			hash := tx.Hash()
//			pool.all.Remove(hash)
//			pool.priced.Removed()
//			oldTxDrop += 1
//		}
//
//		// TODO(thientn): Evaluates enable this.
//		/*
//			// Drop all transactions that are too costly (low balance or out of gas), and queue any invalids back for later
//			drops, invalids := list.Filter(pool.currentState.GetBalance(addr), pool.currentMaxGas)
//			for _, tx := range drops {
//				hash := tx.Hash()
//				pool.logger.Info("Removed unpayable pending transaction", "hash", hash)
//				pool.all.Remove(hash)
//				pool.priced.Removed()
//				pendingNofundsCounter.Inc(1)
//			}
//			for _, tx := range invalids {
//				hash := tx.Hash()
//				pool.logger.Info("Demoting pending transaction", "hash", hash)
//				pool.enqueueTx(hash, tx)
//			}
//		*/
//
//		// If there's a gap in front, alert (should never happen) and postpone all transactions
//		if list.Len() > 0 && list.txs.Get(nonce) == nil {
//			for _, tx := range list.Cap(0) {
//				hash := tx.Hash()
//				pool.logger.Error("Demoting invalidated transaction", "hash", hash)
//				pool.enqueueTx(hash, tx)
//			}
//		}
//		// Delete the entire queue entry if it became empty.
//		if list.Empty() {
//			delete(pool.pending, addr)
//			delete(pool.beats, addr)
//		}
//	}
//	if oldTxDrop > 0 {
//		pool.logger.Info("demoteUnexecutables: Drop txs that are low nonce [likely committed txs]",
//			"number of txs", oldTxDrop,
//			"block height", pool.chain.CurrentBlock().Height())
//	}
//}

// addressByHeartbeat is an account address tagged with its last activity timestamp.
type addressByHeartbeat struct {
	address   common.Address
	heartbeat time.Time
}

type addresssByHeartbeat []addressByHeartbeat

func (a addresssByHeartbeat) Len() int           { return len(a) }
func (a addresssByHeartbeat) Less(i, j int) bool { return a[i].heartbeat.Before(a[j].heartbeat) }
func (a addresssByHeartbeat) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

// accountSet is simply a set of addresses to check for existence
type accountSet struct {
	accounts map[common.Address]struct{}
}

// newAccountSet creates a new address set
func newAccountSet() *accountSet {
	return &accountSet{
		accounts: make(map[common.Address]struct{}),
	}
}

// contains checks if a given address is contained within the set.
func (as *accountSet) contains(addr common.Address) bool {
	_, exist := as.accounts[addr]
	return exist
}

// containsTx checks if the sender of a given tx is within the set. If the sender
// cannot be derived, this method returns false.
func (as *accountSet) containsTx(tx *types.Transaction) bool {
	if addr, err := types.Sender(tx); err == nil {
		return as.contains(addr)
	}
	return false
}

// add inserts a new address into the set to track.
func (as *accountSet) add(addr common.Address) {
	as.accounts[addr] = struct{}{}
}

// txLookup is used internally by TxPool to track transactions while allowing lookup without
// mutex contention.
//
// Note, although this type is properly protected against concurrent access, it
// is **not** a type that should ever be mutated or even exposed outside of the
// transaction pool, since its internal state is tightly coupled with the pools
// internal mechanisms. The sole purpose of the type is to permit out-of-bound
// peeking into the pool in TxPool.Get without having to acquire the widely scoped
// TxPool.mu mutex.
type txLookup struct {
	limit int
	all  map[common.Hash]*types.Transaction
	heap txLookupHeap
	lock sync.RWMutex
}

// newTxLookup returns a new txLookup structure.
func newTxLookup(limit int) *txLookup {
	return &txLookup{
		all: make(map[common.Hash]*types.Transaction),
		heap: make(txLookupHeap, 0),
		limit: limit,
	}
}

// Range calls f on each key and value present in the map.
func (t *txLookup) Range(f func(hash common.Hash, tx *types.Transaction) bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	for key, value := range t.all {
		if !f(key, value) {
			break
		}
	}
}

// Get returns a transaction if it exists in the lookup, or nil if not found.
func (t *txLookup) Get(hash common.Hash) *types.Transaction {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.all[hash]
}

// Count returns the current number of items in the lookup.
func (t *txLookup) Count() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.all)
}

// Add adds a transaction to the lookup.
func (t *txLookup) Add(tx *types.Transaction) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.all[tx.Hash()]; !ok {
		t.all[tx.Hash()] = tx
		t.heap.Push(tx.Hash())

		// loop until heap <= limit
		for {
			if len(t.heap) <= t.limit {
				break
			}

			txHash := t.heap.Pop().(common.Hash)
			delete(t.all, txHash)
		}
	}
}

// Remove removes a transaction from the lookup.
func (t *txLookup) Remove(hash common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.all, hash)
	for i, txHash := range t.heap {
		if txHash == hash {
			newHeap := make(txLookupHeap, 0)
			if i < len(t.heap) - 1 {
				newHeap = t.heap[0:i]
				newHeap = append(newHeap, t.heap[i+1:len(t.heap)]...)
			} else {
				newHeap = t.heap[0:len(t.heap) - 1]
			}
			t.heap = newHeap
			break
		}
	}
}

type txLookupHeap []common.Hash

func (h txLookupHeap) Len() int      { return len(h) }
func (h txLookupHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *txLookupHeap) Push(x interface{}) {
	*h = append(*h, x.(common.Hash))
}

func (h *txLookupHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
