package dev

import (
	"testing"
	"os"
	cmn "github.com/kardiachain/go-kardia/lib/common"
	con "github.com/kardiachain/go-kardia/consensus/types"
	"github.com/kardiachain/go-kardia/lib/log"
)

var votingStrategyTests = []struct {
	Height		*cmn.BigInt
	Round   	*cmn.BigInt
	Step    	con.RoundStepType
	VoteType 	*cmn.BigInt
}{
	{cmn.NewBigInt(1), cmn.NewBigInt(0),4, cmn.NewBigInt(0)},
	{cmn.NewBigInt(1), cmn.NewBigInt(0),6, cmn.NewBigInt(1)},
	{cmn.NewBigInt(1), cmn.NewBigInt(0),4, cmn.NewBigInt(-1)},
}

func TestDevEnvironmentConfig_SetVotingStrategy(t *testing.T) {
	os.Chdir("../..")
	devEnv := CreateDevEnvironmentConfig()
	devEnv.SetVotingStrategy("voting_strategy.csv")
	for i, test := range votingStrategyTests {
		votingStrategy := devEnv.VotingStrategy[i]
		height := test.Height
		round := test.Round
		step := test.Step
		voteType := test.VoteType
		log.Info("I", i)

		if !height.Equals(votingStrategy.Height) {
			t.Errorf("Expected height %v got %v", height, votingStrategy.Height)
		}
		if !round.Equals(votingStrategy.Round) {
			t.Errorf("Expected round %v got %v", round, votingStrategy.Round)
		}
		if step != votingStrategy.Step {
			t.Errorf("Expected step %v got %v", step, votingStrategy.Step)
		}

		if !voteType.Equals(votingStrategy.VoteType) {
			t.Errorf("Expected voteType %v got %v", voteType, votingStrategy.VoteType)
		}

		if voteType.Equals(cmn.NewBigInt(0)) {
			t.Logf("VoteType is %v - No vote", voteType.Value)
		}

		if voteType.Equals(cmn.NewBigInt(1)) {
			t.Logf("VoteType is %v - Good vote", voteType.Value)
		}

		if voteType.Equals(cmn.NewBigInt(-1)) {
			t.Logf("VoteType is %v - Bad vote", voteType.Value)
		}
	}
}