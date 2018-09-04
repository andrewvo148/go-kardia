package state

import (
	"fmt"

	fail "github.com/ebuchman/fail-test"
	cmn "github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/lib/log"
	"github.com/kardiachain/go-kardia/types"
)

// EvidencePool defines the EvidencePool interface used by the ConsensusState.
type EvidencePool interface {
	PendingEvidence() []types.Evidence
}

// ValidateBlock validates the given block against the given state.
// If the block is invalid, it returns an error.
// Validation does not mutate state, but does require historical information from the stateDB,
// ie. to verify evidence from a validator at an old height.
func ValidateBlock(state LastestBlockState, block *types.Block) error {
	return validateBlock(state, block)
}

// ApplyBlock validates the block against the state, and saves the new state.
// It's the only function that needs to be called
// from outside this package to process and commit an entire block.
// It takes a blockID to avoid recomputing the parts hash.
func ApplyBlock(state LastestBlockState, blockID types.BlockID, block *types.Block) (LastestBlockState, error) {
	if err := ValidateBlock(state, block); err != nil {
		return state, ErrInvalidBlock(err)
	}

	fail.Fail() // XXX

	// update the state with the block and responses
	var err error
	state, err = updateState(state, blockID, block.Header())
	if err != nil {
		return state, fmt.Errorf("Commit failed for application: %v", err)
	}

	log.Warn("Update evidence pool.")
	fail.Fail() // XXX

	return state, nil
}

// updateState returns a new State updated according to the header and responses.
func updateState(state LastestBlockState, blockID types.BlockID, header *types.Header) (LastestBlockState, error) {
	log.Trace("updateState", "state", state, "blockID", blockID, "header", header)

	// copy the valset so we can apply changes from EndBlock
	// and update s.LastValidators and s.Validators
	nextValSet := state.Validators.Copy()

	// update the validator set with the latest abciResponses
	lastHeightValsChanged := state.LastHeightValidatorsChanged

	// Update validator accums and set state variables
	nextValSet.IncrementAccum(1)

	var totalTx *cmn.BigInt
	if state.LastBlockTotalTx == nil {
		totalTx = nil
	} else {
		totalTx = state.LastBlockTotalTx.Add(int64(header.NumTxs))
	}

	return LastestBlockState{
		ChainID:                     state.ChainID,
		LastBlockHeight:             cmn.NewBigInt(int64(header.Height)),
		LastBlockTotalTx:            totalTx,
		LastBlockID:                 blockID,
		LastBlockTime:               header.Time,
		Validators:                  nextValSet,
		LastValidators:              state.Validators.Copy(),
		LastHeightValidatorsChanged: lastHeightValsChanged,
	}, nil
}
