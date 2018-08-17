package tool

import (
	"github.com/kardiachain/go-kardia/account"
	"github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/lib/crypto"
	"github.com/kardiachain/go-kardia/types"
	"math/big"
)

// generate an array of random transactions with the numberOfTx (default: 10) and senderAccount, if  sender is Nil, it will use default sender to create tx.
func GenerateRandomTx(numberOfTx int, senderAccount *account.KeyStore) []types.Transaction {
	if numberOfTx <= 0 {
		numberOfTx = 10
	}
	key, _ := crypto.HexToECDSA("45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8")
	if senderAccount != nil {
		key = &senderAccount.PrivateKey
	}
	result := make([]types.Transaction, numberOfTx)
	for i := 0; i < numberOfTx; i++ {
		tx, _ := types.SignTx(types.NewTransaction(
			uint64(i+1),
			common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87"),
			big.NewInt(10),
			22000,
			big.NewInt(10),
			nil,
		), key)
		result[i] = *tx
	}
	return result
}
