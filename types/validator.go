package types

import (
	"bytes"
	"crypto/ecdsa"
	"sort"

	"github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/lib/crypto"
)

// Volatile state for each Validator
type Validator struct {
	Address     common.Address  `json:"address"`
	PubKey      ecdsa.PublicKey `json:"pub_key"`
	VotingPower int64           `json:"voting_power"`

	Accum int64 `json:"accum"`
}

func NewValidator(pubKey ecdsa.PublicKey, votingPower int64) *Validator {
	return &Validator{
		Address:     crypto.PubkeyToAddress(pubKey),
		PubKey:      pubKey,
		VotingPower: votingPower,
		Accum:       0,
	}
}

// Hash computes the unique ID of a validator with a given voting power.
func (v *Validator) Hash() common.Hash {
	return rlpHash(v)
}

// Creates a new copy of the validator.
// Panics if the validator is nil.
func (v *Validator) Copy() *Validator {
	vCopy := *v
	return &vCopy
}

// --------- ValidatorSet ----------

// ValidatorSet represent a set of *Validator at a given height.
// The validators can be fetched by address or index.
// The index is in order of .Address, so the indices are fixed
// for all rounds of a given blockchain height.
// NOTE: Not goroutine-safe.
// NOTE: All get/set to validators should copy the value for safety.
//
// TODO(huny@): The first prototype assumes static set of Validators with round-robin proposer
type ValidatorSet struct {
	// NOTE: persisted via reflect, must be exported.
	Validators []*Validator `json:"validators"`
	Proposer   *Validator   `json:"proposer"`

	// cached (unexported)
	totalVotingPower int64
}

func NewValidatorSet(vals []*Validator) *ValidatorSet {
	validators := make([]*Validator, len(vals))
	for i, val := range vals {
		validators[i] = val.Copy()
	}
	sort.Sort(ValidatorsByAddress(validators))
	vs := &ValidatorSet{
		Validators: validators,
	}

	if vals != nil {
		vs.Proposer = vs.findNextProposer()
	}

	return vs
}

// HasAddress returns true if address given is in the validator set, false -
// otherwise.
func (valSet *ValidatorSet) HasAddress(address common.Address) bool {
	idx := sort.Search(len(valSet.Validators), func(i int) bool {
		return bytes.Compare(address.Bytes(), valSet.Validators[i].Address.Bytes()) <= 0
	})
	return idx < len(valSet.Validators) && bytes.Equal(valSet.Validators[idx].Address.Bytes(), address.Bytes())
}

// GetByAddress returns an index of the validator with address and validator
// itself if found. Otherwise, -1 and nil are returned.
func (valSet *ValidatorSet) GetByAddress(address common.Address) (index int, val *Validator) {
	idx := sort.Search(len(valSet.Validators), func(i int) bool {
		return bytes.Compare(address.Bytes(), valSet.Validators[i].Address.Bytes()) <= 0
	})
	if idx < len(valSet.Validators) && bytes.Equal(valSet.Validators[idx].Address.Bytes(), address.Bytes()) {
		return idx, valSet.Validators[idx].Copy()
	}
	return -1, nil
}

// GetByIndex returns the validator's address and validator itself by index.
// It returns nil values if index is less than 0 or greater or equal to
// len(ValidatorSet.Validators).
func (valSet *ValidatorSet) GetByIndex(index int) (address common.Address, val *Validator) {
	if index < 0 || index >= len(valSet.Validators) {
		return common.BytesToAddress(nil), nil
	}
	val = valSet.Validators[index]
	return val.Address, val.Copy()
}

// Size returns the length of the validator set.
func (valSet *ValidatorSet) Size() int {
	return len(valSet.Validators)
}

// TotalVotingPower returns the sum of the voting powers of all validators.
func (valSet *ValidatorSet) TotalVotingPower() int64 {
	if valSet.totalVotingPower == 0 {
		for _, val := range valSet.Validators {
			// mind overflow
			valSet.totalVotingPower = valSet.totalVotingPower + val.VotingPower
		}
	}
	return valSet.totalVotingPower
}

// GetProposer returns the current proposer. If the validator set is empty, nil
// is returned.
func (valSet *ValidatorSet) GetProposer() (proposer *Validator) {
	if len(valSet.Validators) == 0 {
		return nil
	}
	if valSet.Proposer == nil {
		valSet.Proposer = valSet.findNextProposer()
	}
	return valSet.Proposer.Copy()
}

// Simple round-robin proposer picker
// TODO(huny@): Implement more fancy algo based on accum later
func (valSet *ValidatorSet) findNextProposer() *Validator {
	if valSet.Proposer == nil {
		return valSet.Validators[0]
	}
	for i, val := range valSet.Validators {
		if bytes.Equal(val.Address.Bytes(), valSet.Proposer.Address.Bytes()) {
			if i == valSet.Size()-1 {
				return valSet.Validators[0]
			} else {
				return valSet.Validators[i+1]
			}
		}
	}
	// Reaching here means current proposer is NOT in the set, so return the first validator
	return valSet.Validators[0]
}

// TODO(huny@): Probably use Merkle proof tree with Validators as leaves?
func (valSet *ValidatorSet) Hash() common.Hash {
	return rlpHash(valSet)
}

// Copy each validator into a new ValidatorSet
func (valSet *ValidatorSet) Copy() *ValidatorSet {
	validators := make([]*Validator, len(valSet.Validators))
	for i, val := range valSet.Validators {
		// NOTE: must copy, since IncrementAccum updates in place.
		validators[i] = val.Copy()
	}
	return &ValidatorSet{
		Validators:       validators,
		Proposer:         valSet.Proposer,
		totalVotingPower: valSet.totalVotingPower,
	}
}

// IncrementAccum increments accum of each validator and updates the
// proposer. Panics if validator set is empty.
func (valSet *ValidatorSet) IncrementAccum(times int) {
	// TODO(namdoh): Implement.
	// Add VotingPower * times to each validator and order into heap.
	//validatorsHeap := cmn.NewHeap()
	//for _, val := range valSet.Validators {
	//	// check for overflow both multiplication and sum
	//	val.Accum = safeAddClip(val.Accum, safeMulClip(val.VotingPower, int64(times)))
	//	validatorsHeap.PushComparable(val, accumComparable{val})
	//}
	//
	//// Decrement the validator with most accum times times
	//for i := 0; i < times; i++ {
	//	mostest := validatorsHeap.Peek().(*Validator)
	//	// mind underflow
	//	mostest.Accum = safeSubClip(mostest.Accum, valSet.TotalVotingPower())
	//
	//	if i == times-1 {
	//		valSet.Proposer = mostest
	//	} else {
	//		validatorsHeap.Update(mostest, accumComparable{mostest})
	//	}
	//}

	// For now, allow same node to propose.
	return

}

// Verify that +2/3 of the set had signed the given signBytes
func (valSet *ValidatorSet) VerifyCommit(chainID string, blockID BlockID, height int64, commit *Commit) error {
	panic("validator.VerifyCommit - Not yet implemented")
	return nil
	//if valSet.Size() != len(commit.Precommits) {
	//	return fmt.Errorf("Invalid commit -- wrong set size: %v vs %v", valSet.Size(), len(commit.Precommits))
	//}
	//if height != commit.Height() {
	//	return fmt.Errorf("Invalid commit -- wrong height: %v vs %v", height, commit.Height())
	//}
	//
	//talliedVotingPower := int64(0)
	//round := commit.Round()
	//
	//for idx, precommit := range commit.Precommits {
	//	// may be nil if validator skipped.
	//	if precommit == nil {
	//		continue
	//	}
	//	if precommit.Height != height {
	//		return fmt.Errorf("Invalid commit -- wrong height: %v vs %v", height, precommit.Height)
	//	}
	//	if precommit.Round != round {
	//		return fmt.Errorf("Invalid commit -- wrong round: %v vs %v", round, precommit.Round)
	//	}
	//	if precommit.Type != VoteTypePrecommit {
	//		return fmt.Errorf("Invalid commit -- not precommit @ index %v", idx)
	//	}
	//	_, val := valSet.GetByIndex(idx)
	//	// Validate signature
	//	precommitSignBytes := precommit.SignBytes(chainID)
	//	if !val.PubKey.VerifyBytes(precommitSignBytes, precommit.Signature) {
	//		return fmt.Errorf("Invalid commit -- invalid signature: %v", precommit)
	//	}
	//	if !blockID.Equals(precommit.BlockID) {
	//		continue // Not an error, but doesn't count
	//	}
	//	// Good precommit!
	//	talliedVotingPower += val.VotingPower
	//}
	//
	//if talliedVotingPower > valSet.TotalVotingPower()*2/3 {
	//	return nil
	//}
	//return fmt.Errorf("Invalid commit -- insufficient voting power: got %v, needed %v",
	//	talliedVotingPower, (valSet.TotalVotingPower()*2/3 + 1))
}

//-------------------------------------
// Implements sort for sorting validators by address.

// Sort validators by address
type ValidatorsByAddress []*Validator

func (vs ValidatorsByAddress) Len() int {
	return len(vs)
}

func (vs ValidatorsByAddress) Less(i, j int) bool {
	return bytes.Compare(vs[i].Address.Bytes(), vs[j].Address.Bytes()) == -1
}

func (vs ValidatorsByAddress) Swap(i, j int) {
	it := vs[i]
	vs[i] = vs[j]
	vs[j] = it
}
