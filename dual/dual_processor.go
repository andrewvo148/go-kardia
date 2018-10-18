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

package dual

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/kardiachain/go-kardia/abi"
	bc "github.com/kardiachain/go-kardia/blockchain"
	"github.com/kardiachain/go-kardia/kai/dev"
	"github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/lib/crypto"
	"github.com/kardiachain/go-kardia/lib/event"
	"github.com/kardiachain/go-kardia/lib/log"
	"github.com/kardiachain/go-kardia/state"
	"github.com/kardiachain/go-kardia/tool"
	"github.com/kardiachain/go-kardia/types"
	"github.com/kardiachain/go-kardia/vm"
	"github.com/shopspring/decimal"
)

type DualProcessor struct {
	blockchain        *bc.BlockChain
	txPool            *bc.TxPool
	smcAddress        *common.Address
	smcABI            *abi.ABI
	smcCallSenderAddr common.Address

	// For when running dual node to Eth network
	ethKardia *EthKardia
	// TODO: add struct when running dual node to Neo

	// Chain head subscription for new blocks.
	chainHeadCh  chan bc.ChainHeadEvent
	chainHeadSub event.Subscription
}

func NewDualProcessor(chain *bc.BlockChain, txPool *bc.TxPool, smcAddr *common.Address, smcABIStr string) (*DualProcessor, error) {
	smcABI, err := abi.JSON(strings.NewReader(smcABIStr))
	if err != nil {
		return nil, err
	}

	processor := &DualProcessor{
		blockchain:        chain,
		txPool:            txPool,
		smcAddress:        smcAddr,
		smcABI:            &smcABI,
		smcCallSenderAddr: common.HexToAddress("0x7cefC13B6E2aedEeDFB7Cb6c32457240746BAEe5"),

		chainHeadCh: make(chan bc.ChainHeadEvent, 5),
	}

	// Start subscription to blockchain head event.
	processor.chainHeadSub = chain.SubscribeChainHeadEvent(processor.chainHeadCh)

	return processor, nil
}

func (p *DualProcessor) Start() {
	// Start event loop
	go p.loop()
}

func (p *DualProcessor) RegisterEthDualNode(ethKardia *EthKardia) {
	p.ethKardia = ethKardia
}

func (p *DualProcessor) loop() {
	for {
		select {
		case ev := <-p.chainHeadCh:
			if ev.Block != nil {
				// New block
				// TODO(thietn): concurrency improvement. Consider call new go routine, or have height atomic counter.
				p.checkNewBlock(ev.Block)
			}
		case err := <-p.chainHeadSub.Err():
			log.Error("Error while listening to new blocks", "error", err)
			return
		}
	}
}

// TODO(namdoh, #115): Rename this to handleBlock to be consistent with eth.go
func (p *DualProcessor) checkNewBlock(block *types.Block) {
	smcUpdate := false
	for _, tx := range block.Transactions() {
		if tx.To() != nil && *tx.To() == *p.smcAddress {
			// New tx that updates smc, check input method for more filter.
			method, err := p.smcABI.MethodById(tx.Data()[0:4])
			if err != nil {
				log.Error("Fail to unpack smc update method in tx", "tx", tx, "error", err)
				return
			}
			log.Info("Detect tx updating smc", "method", method.Name, "Value", tx.Value())
			if method.Name == "removeEth" || method.Name == "removeNeo" {
				// Not set flag here. If the block contains only the removeEth/removeNeo, skip look up the amount to avoid infinite loop.
				log.Info("Skip tx updating smc to remove Eth/Neo", "method", method.Name)
				continue
			}
			smcUpdate = true
		}
	}
	if !smcUpdate {
		return
	}
	log.Info("Detect smc update, running VM call to check sending value")

	statedb, err := p.blockchain.StateAt(block.Root())
	if err != nil {
		log.Error("Error getting block state in dual process", "height", block.Height())
		return
	}

	// Trigger the logic depend on what type of dual node
	// In the future this can be a common interface with a single method
	if p.ethKardia != nil {
		// Eth dual node
		ethSendValue := p.CallKardiaMasterGetEthToSend(p.smcCallSenderAddr, statedb)
		log.Info("Kardia smc calls getEthToSend", "eth", ethSendValue)
		if ethSendValue != nil && ethSendValue.Cmp(big.NewInt(0)) != 0 {
			// TODO(namdoh, #115): Remember this txs event to dual service's pool

			// TODO(namdoh, #115): Split this into two part 1) create an Ether tx and propose it and 2)
			// once its block is commit, the next proposer will submit it.
			// Create and submit a Kardia tx.
			p.ethKardia.SendEthFromContract(ethSendValue)

			// Create Kardia tx removeEth right away to acknowledge the ethsend
			gAccount := "0xe94517a4f6f45e80CbAaFfBb0b845F4c0FDD7547"
			addrKeyBytes, _ := hex.DecodeString(dev.GenesisAddrKeys[gAccount])
			addrKey := crypto.ToECDSAUnsafe(addrKeyBytes)

			// TODO(namdoh, #115): Split this into two part 1) create a Kardia tx and propose it and 2)
			// once its block is commit, the next proposer will execute it.
			// Create and submit a Kardia tx.
			tx := CreateKardiaRemoveAmountTx(addrKey, statedb, ethSendValue, 1)
			if err := p.txPool.AddLocal(tx); err != nil {
				log.Error("Fail to add Kardia tx to removeEth", err, "tx", tx)
			} else {
				log.Info("Creates removeEth tx", tx.Hash().Hex())
			}
		}
	} else {
		// Neo dual node
		neoSendValue := p.CallKardiaMasterGetNeoToSend(p.smcCallSenderAddr, statedb)
		log.Info("Kardia smc calls getNeoToSend", "neo", neoSendValue)
		if neoSendValue != nil && neoSendValue.Cmp(big.NewInt(0)) != 0 {
			// TODO: create new NEO tx to send NEO
			// Temporarily hard code the recipient
			amountToRelease := decimal.NewFromBigInt(neoSendValue, 10).Div(decimal.NewFromBigInt(common.BigPow(10, 18), 10))
			log.Info("Original amount neo to release", "amount", amountToRelease, "neodual", "neodual")
			convertedAmount := amountToRelease.Mul(decimal.NewFromBigInt(big.NewInt(10), 0))
			log.Info("Converted amount to release", "converted", convertedAmount, "neodual", "neodual")
			if convertedAmount.LessThan(decimal.NewFromFloat(1.0)) {
				log.Info("Too little amount to send", "amount", convertedAmount, "neodual", "neodual")
			} else {
				// temporarily hard code for the exchange rate
				log.Info("Sending to neo", "amount", convertedAmount, "neodual", "neodual")
				go p.ReleaseNeo(dev.NeoReceiverAddress, big.NewInt(convertedAmount.IntPart()))
				// Create Kardia tx removeNeo to acknowledge the neosend, otherwise getEthToSend will keep return >0
				gAccount := "0xBA30505351c17F4c818d94a990eDeD95e166474b"
				addrKeyBytes, _ := hex.DecodeString(dev.GenesisAddrKeys[gAccount])
				addrKey := crypto.ToECDSAUnsafe(addrKeyBytes)

				tx := CreateKardiaRemoveAmountTx(addrKey, statedb, neoSendValue, 2)
				if err := p.txPool.AddLocal(tx); err != nil {
					log.Error("Fail to add Kardia tx to removeNeo", err, "tx", tx, "neodual", "neodual")
				} else {
					log.Info("Creates removeNeo tx", tx.Hash().Hex(), "neodual", "neodual")
				}
			}
		}
	}
}

func (p *DualProcessor) CallKardiaMasterGetEthToSend(from common.Address, statedb *state.StateDB) *big.Int {
	getEthToSend, err := p.smcABI.Pack("getEthToSend")
	if err != nil {
		log.Error("Fail to pack Kardia smc getEthToSend", "error", err)
		return big.NewInt(0)
	}
	ret, err := callStaticKardiaMasterSmc(from, *p.smcAddress, p.blockchain, getEthToSend, statedb)
	if err != nil {
		log.Error("Error calling master exchange contract", "error", err)
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(ret)
}

func (p *DualProcessor) CallKardiaMasterGetNeoToSend(from common.Address, statedb *state.StateDB) *big.Int {
	getNeoToSend, err := p.smcABI.Pack("getNeoToSend")
	if err != nil {
		log.Error("Fail to pack Kardia smc getEthToSend", "error", err, "neodual", "neodual")
		return big.NewInt(0)
	}
	ret, err := callStaticKardiaMasterSmc(from, *p.smcAddress, p.blockchain, getNeoToSend, statedb)
	if err != nil {
		log.Error("Error calling master exchange contract", "error", err, "neodual", "neodual")
		return big.NewInt(0)
	}

	return new(big.Int).SetBytes(ret)
}

// The following function is just call the master smc and return result in bytes format
func callStaticKardiaMasterSmc(from common.Address, to common.Address, blockchain *bc.BlockChain, input []byte, statedb *state.StateDB) (result []byte, err error) {
	context := bc.NewKVMContextFromDualNodeCall(from, blockchain.CurrentHeader(), blockchain)
	vmenv := vm.NewKVM(context, statedb, vm.Config{})
	sender := vm.AccountRef(from)
	ret, _, err := vmenv.StaticCall(sender, to, input, uint64(100000))
	if err != nil {
		return make([]byte, 0), err
	}
	return ret, nil
}

// CreateKardiaMatchAmountTx creates Kardia tx to report new matching amount from Eth/Neo network.
// type = 1: ETH
// type = 2: NEO
// TODO(namdoh@): Make type of matchType an enum instead of an int.
func CreateKardiaMatchAmountTx(senderKey *ecdsa.PrivateKey, statedb *state.StateDB, quantity *big.Int, matchType int) *types.Transaction {
	masterSmcAddr := dev.GetContractAddressAt(2)
	masterSmcAbi := dev.GetContractAbiByAddress(masterSmcAddr.String())
	kABI, err := abi.JSON(strings.NewReader(masterSmcAbi))

	if err != nil {
		log.Error("Error reading abi", "err", err)
	}
	var getAmountToSend []byte
	if matchType == 1 {
		getAmountToSend, err = kABI.Pack("matchEth", quantity)
	} else {
		getAmountToSend, err = kABI.Pack("matchNeo", quantity)
	}

	if err != nil {
		log.Error("Error getting abi", "error", err, "address", masterSmcAddr, "dual", "dual")

	}
	return tool.GenerateSmcCall(senderKey, masterSmcAddr, getAmountToSend, statedb)
}

// Call to remove amount of ETH / NEO on master smc
// type = 1: ETH
// type = 2: NEO

func CreateKardiaRemoveAmountTx(senderKey *ecdsa.PrivateKey, statedb *state.StateDB, quantity *big.Int, matchType int) *types.Transaction {
	masterSmcAddr := dev.GetContractAddressAt(2)
	masterSmcAbi := dev.GetContractAbiByAddress(masterSmcAddr.String())
	abi, err := abi.JSON(strings.NewReader(masterSmcAbi))

	if err != nil {
		log.Error("Error reading abi", "err", err)
	}
	var amountToRemove []byte
	if matchType == 1 {
		amountToRemove, err = abi.Pack("removeEth", quantity)
	} else {
		amountToRemove, err = abi.Pack("removeNeo", quantity)
		log.Info("byte to send to remove", "byte", string(amountToRemove), "neodual", "neodual")
	}

	if err != nil {
		log.Error("Error getting abi", "error", err, "address", masterSmcAddr, "dual", "dual")

	}
	return tool.GenerateSmcCall(senderKey, masterSmcAddr, amountToRemove, statedb)
}

// Call Api to release Neo
func CallReleaseNeo(address string, amount *big.Int) (string, error) {
	body := []byte(`{
  "jsonrpc": "2.0",
  "method": "dual_sendeth",
  "params": ["` + address + `",` + amount.String() + `],
  "id": 1
}`)
	log.Info("Release neo", "message", string(body), "neodual", "neodual")
	var submitUrl string
	if dev.IsUsingNeoTestNet {
		submitUrl = dev.TestnetNeoSubmitUrl
	} else {
		submitUrl = dev.NeoSubmitTxUrl
	}
	rs, err := http.Post(submitUrl, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	bytesRs, err := ioutil.ReadAll(rs.Body)
	if err != nil {
		return "", err
	}
	var f interface{}
	json.Unmarshal(bytesRs, &f)
	if f == nil {
		return "", errors.New("Nil return")
	}
	m := f.(map[string]interface{})
	var txid string
	if m["result"] == nil {
		return "", errors.New("Nil return")
	}
	txid = m["result"].(string)
	log.Info("tx result neo", "txid", txid, "neodual", "neodual")
	return txid, nil
}

// Call Neo api to check status
// txid is "not found" : pending tx or tx is failed, need to loop checking
// to cover both case
func checkTxNeo(txid string) bool {
	log.Info("Checking tx id", "txid", txid)
	var checkTxUrl string
	if dev.IsUsingNeoTestNet {
		checkTxUrl = dev.TestnetNeoCheckTxUrl
	} else {
		checkTxUrl = dev.NeoCheckTxUrl
	}
	url := checkTxUrl + txid
	rs, err := http.Get(url)
	if err != nil {
		return false
	}
	bytesRs, err := ioutil.ReadAll(rs.Body)
	if err != nil {
		return false
	}
	var f interface{}
	json.Unmarshal(bytesRs, &f)

	if f == nil {
		return false
	}

	m := f.(map[string]interface{})

	if m["txid"] == nil {
		return false
	}

	txid = m["txid"].(string)
	if txid != "not found" {
		log.Info("Checking tx result neo", "txid", txid, "neodual", "neodual")
		return true
	} else {
		log.Info("Checking tx result neo failed", "neodual", "neodual")
		return false
	}
}

// retry send and loop checking tx status until it is successful
func retryTx(address string, amount *big.Int) {
	attempt := 0
	interval := 30
	for {
		log.Info("retrying tx ...", "addr", address, "amount", amount, "neodual", "neodual")
		txid, err := CallReleaseNeo(address, amount)
		if err == nil && txid != "fail" {
			log.Info("Send successfully", "txid", txid, "neodual", "neodual")
			result := loopCheckingTx(txid)
			if result {
				log.Info("tx is successful", "neodual", "neodual")
				return
			} else {
				log.Info("tx is not successful, retry in 5 sconds", "txid", txid, "neodual", "neodual")
			}
		} else {
			log.Info("Posting tx failed, retry in 5 seconds", "txid", txid, "neodual", "neodual")
		}
		attempt++
		if attempt > 1 {
			log.Info("Trying 2 time but still fail, give up now", "txid", txid, "neodual", "neodual")
			return
		}
		sleepDuration := time.Duration(interval) * time.Second
		time.Sleep(sleepDuration)
		interval += 30
	}
}

// Continually check tx status for 10 times, interval is 10 seconds
func loopCheckingTx(txid string) bool {
	attempt := 0
	for {
		time.Sleep(10 * time.Second)
		attempt++
		success := checkTxNeo(txid)
		if !success && attempt > 10 {
			log.Info("Tx fail, need to retry", "attempt", attempt, "neodual", "neodual")
			return false
		}

		if success {
			log.Info("Tx is successful", "txid", txid, "neodual", "neodual")
			return true
		}
	}
}

func (p *DualProcessor) ReleaseNeo(address string, amount *big.Int) {
	log.Info("Release: ", "amount", amount, "address", address, "neodual", "neodual")
	txid, err := CallReleaseNeo(address, amount)
	if err != nil {
		log.Error("Error calling rpc", "err", err, "neodual", "neodual")
	}
	log.Info("Tx submitted", "txid", txid, "neodual", "neodual")
	if txid == "fail" || txid == "" {
		log.Info("Failed to release, retry tx", "txid", txid)
		retryTx(address, amount)
	} else {
		txStatus := loopCheckingTx(txid)
		if !txStatus {
			retryTx(address, amount)
		}
	}
}
