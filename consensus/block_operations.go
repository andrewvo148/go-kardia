package consensus

import (
	"sync"

	"github.com/kardiachain/go-kardia/blockchain"
	"github.com/kardiachain/go-kardia/lib/log"
	"github.com/kardiachain/go-kardia/types"
	cmn "github.com/kardiachain/go-kardia/lib/common"
)

// TODO(thientn/namdoh): this is similar to execution.go & validation.go in state/
// These files should be consolidated in the future.

type BlockOperations struct {
	mtx    sync.RWMutex

	blockchain *blockchain.BlockChain
	txPool *blockchain.TxPool
	height uint64
}

// NewBlockOperations returns a new BlockOperations with latest chain & ,
// initialized to the last height that was committed to the DB.
func NewBlockOperations(blockchain *blockchain.BlockChain, txPool *blockchain.TxPool) *BlockOperations {
	return &BlockOperations{
		blockchain:     blockchain,
		txPool: txPool,
		height: blockchain.CurrentHeader().Height,
	}
}

func (bs *BlockOperations) Height() uint64 {
    return bs.height
}

// SaveBlock persists the given block, blockParts, and seenCommit to the underlying db.
// seenCommit: The +2/3 precommits that were seen which committed at height.
//             If all the nodes restart after committing a block,
//             we need this to reload the precommits to catch-up nodes to the
//             most recent height.  Otherwise they'd stall at H-1.
func (bs *BlockOperations) SaveBlock(block *types.Block, seenCommit *types.Commit) {
	if block == nil {
		cmn.PanicSanity("BlockStore can only save a non-nil block")
	}
	height := block.Height()
	if g, w := height, bs.Height()+1; g != w {
		cmn.PanicSanity(cmn.Fmt("BlockStore can only save contiguous blocks. Wanted %v, got %v", w, g))
	}

	// Save block
	if height != bs.Height()+1 {
		cmn.PanicSanity(cmn.Fmt("BlockStore can only save contiguous blocks. Wanted %v, got %v", bs.Height()+1, height))
	}
	
	bs.blockchain.WriteBlockWithoutState(block)

	// Save block commit (duplicate and separate from the Block)
	bs.blockchain.WriteCommit(height-1, block.LastCommit())

	// Save seen commit (seen +2/3 precommits for block)
	// NOTE: we can delete this at a later height
	bs.blockchain.WriteCommit(height, seenCommit)

	// TODO(thientn/namdoh): remove the committed transactions from tx pool.

	// Done!
	bs.mtx.Lock()
	bs.height = height
	bs.mtx.Unlock()
}

// CollectTransactions queries list of pending transactions from tx pool.
func (b *BlockOperations) CollectTransactions() []*types.Transaction {
	pending, err := b.txPool.Pending()
	if err != nil {
		log.Error("Fail to get pending txns", "err", err)
		return nil
	}

	// TODO: do basic verification & check with gas & sort by nonce
	// check code NewTransactionsByPriceAndNonce
	pendingTxns := make([]*types.Transaction, 0)
	for _, txns := range pending {
		for _, txn := range txns {
			pendingTxns = append(pendingTxns, txn)
		}
	}
	return pendingTxns
}

// GenerateNewAccountStates generates new accountStates by executing given txns on the account state of blockchain head.
func (b *BlockOperations) GenerateNewAccountStates(txns []*types.Transaction) (*types.AccountStates, error) {
	// use accountState of latest block
	accounts := b.blockchain.CurrentBlock().Accounts()
	return blockchain.ApplyTransactionsToAccountState(txns, accounts)
}