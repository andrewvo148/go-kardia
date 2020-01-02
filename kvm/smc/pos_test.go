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

package kvm

import (
	"fmt"
	"github.com/kardiachain/go-kardia/kai/pos"
	"github.com/kardiachain/go-kardia/kai/state"
	"github.com/kardiachain/go-kardia/lib/abi"
	"github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/mainchain/blockchain"
	"github.com/stretchr/testify/require"
	"math/big"
	"strings"
	"testing"
)

func testDeployStaker(t *testing.T, bc *blockchain.BlockChain, st *state.StateDB, node map[string]interface{}) {
	stakerAbi, err := abi.JSON(strings.NewReader(StakerAbi))
	require.NoError(t, err)
	owner := common.HexToAddress(node["owner"].(string))
	staker := common.HexToAddress(node["staker"].(string))
	input, err := stakerAbi.Pack("", masterAddress)
	require.NoError(t, err)
	newStakerCode := append(StakerByteCode, input...)
	_, _, _, err = create(owner, staker, bc.CurrentHeader(), bc, newStakerCode, big.NewInt(0), st)
	require.NoError(t, err)

	// add staker to master
	testAddStakerToMaster(t, bc, st, staker)
}

func testDeployDualMasterSmartContract(t *testing.T, dualMasterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, consensusPeriod uint64, maxValidators uint64, maxViolatePercentage uint64) {
	input, err := dualMasterAbi.Pack("", "ETH", consensusPeriod, maxValidators, maxViolatePercentage)
	require.NoError(t, err)
	sender := common.HexToAddress(genesisNodes[0]["owner"].(string))
	newCode := append(DualMasterByteCode, input...)
	_, _, _, err = create(sender, dualMasterAddress, bc.CurrentHeader(), bc, newCode, big.NewInt(0), st)
	require.NoError(t, err)

	// set genesis to DualMaster
	input, err = dualMasterAbi.Pack("setGenesis", common.HexToAddress(genesisNodes[0]["address"].(string)), common.HexToAddress(genesisNodes[0]["owner"].(string)), minimumStakes)
	require.NoError(t, err)
	output, err := call(common.HexToAddress(genesisNodes[0]["address"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	input, err = dualMasterAbi.Pack("setGenesis", common.HexToAddress(genesisNodes[1]["address"].(string)), common.HexToAddress(genesisNodes[1]["owner"].(string)), minimumStakes)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[0]["address"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	input, err = dualMasterAbi.Pack("setGenesis", common.HexToAddress(genesisNodes[2]["address"].(string)), common.HexToAddress(genesisNodes[2]["owner"].(string)), minimumStakes)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[0]["address"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))
}

func testStake(t *testing.T, bc *blockchain.BlockChain, st *state.StateDB, node map[string]interface{}, target *common.Address, stakeAmount, expectedStakes *big.Int) {
	address := common.HexToAddress(node["address"].(string))
	if target != nil {
		address = *target
	}
	staker := common.HexToAddress(node["staker"].(string))
	owner := common.HexToAddress(node["owner"].(string))

	println(fmt.Sprintf("testStake address:%v staker:%v owner:%v", address.Hex(), staker.Hex(), owner.Hex()))

	stakerAbi, err := abi.JSON(strings.NewReader(StakerAbi))
	require.NoError(t, err)

	stakeInput, err := stakerAbi.Pack("stake", address)
	require.NoError(t, err)

	_, err = call(owner, staker, bc.CurrentHeader(), bc, stakeInput, stakeAmount, st)
	require.NoError(t, err)

	getStakeAmount, err := stakerAbi.Pack("getStakeAmount", address)
	require.NoError(t, err)

	result, err := staticCall(owner, staker, bc.CurrentHeader(), bc, getStakeAmount, st)
	require.NoError(t, err)

	type data struct {
		Amount *big.Int `abi:"amount"`
		StartedAt *big.Int `abi:"startedAt"`
		Valid  bool `abi:"valid"`
	}
	var actualData data
	err = stakerAbi.Unpack(&actualData, "getStakeAmount", result)
	require.NoError(t, err)

	expectedData := data {
		Amount: expectedStakes,
		StartedAt: big.NewInt(0),
		Valid: true,
	}

	require.Equal(t, expectedData.Amount.String(), actualData.Amount.String())
	require.Equal(t, expectedData.Valid, actualData.Valid)
	require.Equal(t, expectedData.StartedAt.String(), actualData.StartedAt.String())
}

func testDeployNodesAndStakes(t *testing.T, bc *blockchain.BlockChain, st *state.StateDB, nodes []map[string]interface{}, isStake bool) {
	nodeAbi, err := abi.JSON(strings.NewReader(NodeAbi))
	require.NoError(t, err)

	for _, node := range nodes {
		addressHex := node["address"].(string)
		owner := node["owner"].(string)
		id := node["id"].(string)
		name := node["name"].(string)
		percentageReward := node["percentageReward"].(uint16)

		input, err := nodeAbi.Pack("", masterAddress, id, name, percentageReward, uint64(100), minimumStakes)
		require.NoError(t, err)

		newCode := append(NodeByteCode, input...)
		address := common.HexToAddress(addressHex)
		// Setup contract code into genesis state
		_, _, _, err = create(common.HexToAddress(owner), address, bc.CurrentHeader(), bc, newCode, big.NewInt(0), st)
		require.NoError(t, err)

		// add node to master
		testAddNodeToMaster(t, bc, st, address)
		testDeployStaker(t, bc, st, node)
		if isStake {
			testStake(t, bc, st, node, nil, minimumStakes, minimumStakes)
		}
	}
}

func testAddNodeToMaster(t *testing.T, bc *blockchain.BlockChain, st *state.StateDB, node common.Address) {
	var (
		masterAbi abi.ABI
		err error
		input []byte
	)
	masterAbi, err = abi.JSON(strings.NewReader(MasterAbi))
	require.NoError(t, err)

	input, err = masterAbi.Pack("addNode", node)
	require.NoError(t, err)

	_, err = call(posHandlerAddress, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)
}

func testAddStakerToMaster(t *testing.T, bc *blockchain.BlockChain, st *state.StateDB, staker common.Address) {
	var (
		masterAbi abi.ABI
		err error
		input []byte
	)
	masterAbi, err = abi.JSON(strings.NewReader(MasterAbi))
	require.NoError(t, err)

	input, err = masterAbi.Pack("addStaker", staker)
	require.NoError(t, err)

	_, err = call(posHandlerAddress, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)
}

func testAvailableNodes(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, expectedLen uint64) {
	sender := common.HexToAddress(genesisNodes[0]["owner"].(string))
	getTotalAvailableNodes, err := masterAbi.Pack("getTotalAvailableNodes")
	require.NoError(t, err)

	result, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, getTotalAvailableNodes, st)
	require.NoError(t, err)

	var totalAvailableNodes *big.Int
	err = masterAbi.Unpack(&totalAvailableNodes, "getTotalAvailableNodes", result)
	require.NoError(t, err)
	require.Equal(t, expectedLen, totalAvailableNodes.Uint64())

	for i:=uint64(1); i<=totalAvailableNodes.Uint64(); i++ {
		input, err := masterAbi.Pack("getAvailableNode", big.NewInt(0).SetUint64(i))
		require.NoError(t, err)
		output, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
		require.NoError(t, err)
		type nodeInfo struct {
			NodeAddress common.Address `abi:"nodeAddress"`
			Owner common.Address `abi:"owner"`
			DualIndex uint64 `abi:"dualIndex"`
			Stakes *big.Int `abi:"stakes"`
		}
		var info nodeInfo
		err = masterAbi.Unpack(&info, "getAvailableNode", output)
		require.NoError(t, err)
		println(fmt.Sprintf("available node by index - index:%v node:%v owner:%v stakes:%v", i, info.NodeAddress.Hex(), info.Owner.Hex(), info.Stakes.String()))
		testGetAvailableNodeIndex(t, masterAbi, bc, st, info.NodeAddress, i)
	}
}

func testGetAvailableNodeIndex(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, node common.Address, expectedIndex uint64) {
	input, err := masterAbi.Pack("getAvailableNodeIndex", node)
	require.NoError(t, err)
	output, err := staticCall(node, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)
	var index *big.Int
	err = masterAbi.Unpack(&index, "getAvailableNodeIndex", output)
	require.NoError(t, err)
	require.Equal(t, expectedIndex, index.Uint64())
}

func testCreateMaster(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, consensusPeriod uint64, maxValidators uint64, maxViolatePercentage uint64) {
	input, err := masterAbi.Pack("", consensusPeriod, maxValidators, maxViolatePercentage)
	require.NoError(t, err)
	sender := common.HexToAddress(genesisNodes[0]["owner"].(string))
	newCode := append(MasterByteCode, input...)
	_, _, _, err = create(sender, masterAddress, bc.CurrentHeader(), bc, newCode, genesisAmount, st)
	require.NoError(t, err)

	// check _availableNodes
	testAvailableNodes(t, masterAbi, bc, st, uint64(len(genesisNodes)))
}

func testGetTotalStakes(t *testing.T, kAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, expected *big.Int) {
	for _, node := range genesisNodes {
		getTotalStakes, err := kAbi.Pack("getTotalStakes", common.HexToAddress(node["address"].(string)))
		require.NoError(t, err)

		result, err := staticCall(common.HexToAddress(node["owner"].(string)), masterAddress, bc.CurrentHeader(), bc, getTotalStakes, st)
		require.NoError(t, err)

		var actual *big.Int
		err = kAbi.Unpack(&actual, "getTotalStakes", result)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func testCollectValidators(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB) {
	input, err := masterAbi.Pack("collectValidators")
	require.NoError(t, err)

	sender := common.HexToAddress(genesisNodes[0]["owner"].(string))
	senderNode := common.HexToAddress(genesisNodes[0]["address"].(string))

	_, err = call(sender, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)

	isValidator, err := masterAbi.Pack("isValidator", sender)
	require.NoError(t, err)

	result, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, isValidator, st)
	require.NoError(t, err)

	var actual bool
	err = masterAbi.Unpack(&actual, "isValidator", result)
	require.Equal(t, true, actual)

	isValidator, err = masterAbi.Pack("isValidator", senderNode)
	require.NoError(t, err)

	result, err = staticCall(sender, masterAddress, bc.CurrentHeader(), bc, isValidator, st)
	require.NoError(t, err)

	err = masterAbi.Unpack(&actual, "isValidator", result)
	require.Equal(t, true, actual)
}

func testGetLatestValidators(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, expectedValidatorsLength uint64, expectedNodes []map[string]interface{}) {
	println("running testGetLatestValidators")
	sender := common.HexToAddress(genesisNodes[0]["owner"].(string))

	getLatestValidatorsInfo, err := masterAbi.Pack("getLatestValidatorsInfo")
	require.NoError(t, err)

	result, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, getLatestValidatorsInfo, st)
	require.NoError(t, err)
	type getLatestValidatorsInfoType struct {
		TotalNodes uint64 `abi:"totalNodes"`
		StartAtBlock uint64 `abi:"startAtBlock"`
		EndAtBlock uint64 `abi:"endAtBlock"`
	}
	var validatorsInfo getLatestValidatorsInfoType
	err = masterAbi.Unpack(&validatorsInfo, "getLatestValidatorsInfo", result)
	require.NoError(t, err)
	require.Equal(t, expectedValidatorsLength, validatorsInfo.TotalNodes)

	for i:=uint64(1); i < validatorsInfo.TotalNodes; i++ {
		getLatestValidator, err := masterAbi.Pack("getLatestValidatorByIndex", i)
		require.NoError(t, err)

		result, err = staticCall(sender, masterAddress, bc.CurrentHeader(), bc, getLatestValidator, st)
		require.NoError(t, err)
		type validator struct {
			Node common.Address `abi:"node"`
			Owner common.Address `abi:"owner"`
			Stakes *big.Int `abi:"stakes"`
			TotalStaker uint64 `abi:"totalStaker"`
		}
		var actual validator
		err = masterAbi.Unpack(&actual, "getLatestValidatorByIndex", result)
		require.NoError(t, err)

		node := expectedNodes[i-1]
		println(fmt.Sprintf("node:%v owner:%v stakes:%v totalStaker:%v", actual.Node.Hex(), actual.Owner.Hex(), actual.Stakes, actual.TotalStaker))
		require.Equal(t, node["address"].(string), actual.Node.Hex())
		require.Equal(t, node["owner"].(string), actual.Owner.Hex())
		require.Equal(t, node["expectedStakes"].(*big.Int), actual.Stakes)
		require.Equal(t, node["expectedStaker"].(uint64), actual.TotalStaker)
	}
}

func testGetPendingNode(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB,
	index uint64, expectedAddress common.Address, expectedVote uint64) {
	input, err := masterAbi.Pack("getPendingNode", index)
	require.NoError(t, err)

	output, err := staticCall(common.HexToAddress(genesisNodes[0]["owner"].(string)), masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	type pendingNode struct {
		NodeAddress common.Address `abi:"nodeAddress"`
		Stakes *big.Int `abi:"stakes"`
		Vote uint64 `abi:"vote"`
	}
	var outputNode pendingNode
	err = masterAbi.Unpack(&outputNode, "getPendingNode", output)
	require.NoError(t, err)

	if outputNode.NodeAddress.Equal(common.HexToAddress("0x")) {
		return
	}

	expectedNode := pendingNode{
		NodeAddress: expectedAddress,
		Stakes:      big.NewInt(0),
		Vote:        expectedVote,
	}

	require.NotNil(t, outputNode)
	println(fmt.Sprintf("finish getting pending node address:%v vote:%v", outputNode.NodeAddress.Hex(), outputNode.Vote))

	require.Equal(t, expectedNode.NodeAddress.Hex(), outputNode.NodeAddress.Hex())
	require.Equal(t, expectedNode.Stakes.String(), outputNode.Stakes.String())
	require.Equal(t, expectedNode.Vote, outputNode.Vote)
}

func testAddPendingNode(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, node map[string]interface{}, sender common.Address) {
	address := common.HexToAddress(node["address"].(string))
	owner := common.HexToAddress(node["owner"].(string))

	input, err := masterAbi.Pack("addPendingNode", address)
	require.NoError(t, err)

	_, err = call(sender, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)

	input, err = masterAbi.Pack("getTotalPending")
	require.NoError(t, err)

	output, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	var result *big.Int
	err = masterAbi.Unpack(&result, "getTotalPending", output)
	require.NoError(t, err)
	require.Equal(t, true, result.Uint64() > 0)

	println(fmt.Sprintf("finish testAddPendingNode sender:%v address:%v owner:%v", sender.Hex(), address.Hex(), owner.Hex()))
	//testGetPendingNode(t, masterAbi, bc, st, result, address, uint64(1))
}

func testVotePending(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, nodes []map[string]interface{},
		expectedAvailableLen uint64) {
	sender := common.HexToAddress(genesisNodes[0]["owner"].(string))
	// get latest pending node
	input, err := masterAbi.Pack("getTotalPending")
	require.NoError(t, err)

	output, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	var index *big.Int
	err = masterAbi.Unpack(&index, "getTotalPending", output)
	require.NoError(t, err)
	require.Equal(t, true, index.Uint64() > 0)

	for _, node := range nodes {
		input, err = masterAbi.Pack("votePending", index.Uint64())
		require.NoError(t, err)
		println(fmt.Sprintf("voting for index:%v sender:%v", index.Uint64(), node["owner"].(string)))
		_, err = call(common.HexToAddress(node["owner"].(string)), masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
		if err != nil {
			// try to get pending node by index, if it is found then throw t.Fatal
			input, err = masterAbi.Pack("getPendingNode", index.Uint64())
			require.NoError(t, err)
			output, err = staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
			require.NoError(t, err)
			type pendingNode struct {
				NodeAddress common.Address `abi:"nodeAddress"`
				Stakes *big.Int `abi:"stakes"`
				Vote uint64 `abi:"vote"`
			}
			var outputNode pendingNode
			err = masterAbi.Unpack(&outputNode, "getPendingNode", output)
			require.NoError(t, err)
			if !outputNode.NodeAddress.Equal(common.HexToAddress("0x")) {
				t.Fatal("expected pending node does not exist, but got existed")
			}
		}
	}
	if expectedAvailableLen > 0 {
		// check available nodes
		testAvailableNodes(t, masterAbi, bc, st, expectedAvailableLen)
	}
}

func testHasPendingVoted(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, sender common.Address, index uint64, expected bool) {
	input, err := masterAbi.Pack("hasPendingVoted", index)
	require.NoError(t, err)

	output, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	var result bool
	err = masterAbi.Unpack(&result, "hasPendingVoted", output)
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func testRequestDelete(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, index uint64, sender, expectedNode common.Address) {
	input, err := masterAbi.Pack("requestDelete", index)
	require.NoError(t, err)

	_, err = call(sender, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)

	testGetRequestDelete(t, masterAbi, bc, st, uint64(1), index, uint64(1), sender, expectedNode)
}

func testGetRequestDelete(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, index, expectedIndex, expectedVote uint64, sender, expectedNode common.Address) {
	input, err := masterAbi.Pack("getRequestDeleteNode", index)
	require.NoError(t, err)
	output, err := staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	type deleteInfo struct {
		NodeIndex uint64 `abi:"nodeIndex"`
		NodeAddress common.Address `abi:"nodeAddress"`
		Stakes *big.Int `abi:"stakes"`
		Vote uint64 `abi:"vote"`
	}
	var info deleteInfo
	err = masterAbi.Unpack(&info, "getRequestDeleteNode", output)
	require.NoError(t, err)

	println(fmt.Sprintf("testGetRequestDelete index:%v nodeIndex:%v nodeAddress:%v stakes:%v vote:%v", index, info.NodeIndex, info.NodeAddress.Hex(), info.Stakes.Uint64(), info.Vote))
	require.Equal(t, expectedNode.Hex(), info.NodeAddress.Hex())
	require.Equal(t, expectedIndex, info.NodeIndex)
	require.Equal(t, expectedVote, info.Vote)
}

func testVoteDeleteNode(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, index, expectedIndex, expectedVote uint64, sender, expectedNode common.Address) {
	method := "voteDeleting"
	input, err := masterAbi.Pack(method, index)
	require.NoError(t, err)
	_, err = call(sender, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)

	testGetRequestDelete(t, masterAbi, bc, st, index, expectedIndex, expectedVote, sender, expectedNode)
}

func testWithdraw(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB, node, staker common.Address, amount *big.Int, expectedNewIndex uint64) {
	type nodeInfo struct {
		NodeAddress common.Address `abi:"nodeAddress"`
		Owner common.Address `abi:"owner"`
		Stakes *big.Int `abi:"stakes"`
		DualIndex uint64 `abi:"dualIndex"`
	}

	var (
		before, after nodeInfo
		input, output []byte
		err error
	)

	println("start testWithdraw")

	input, err = masterAbi.Pack("getAvailableNode", big.NewInt(0).SetUint64(expectedNewIndex))
	require.NoError(t, err)
	output, err = staticCall(staker, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)
	err = masterAbi.Unpack(&before, "getAvailableNode", output)

	input, err = masterAbi.Pack("withdraw", node, amount)
	require.NoError(t, err)

	_, err = call(staker, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)

	input, err = masterAbi.Pack("getAvailableNode", big.NewInt(0).SetUint64(expectedNewIndex))
	require.NoError(t, err)
	output, err = staticCall(staker, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)
	err = masterAbi.Unpack(&after, "getAvailableNode", output)
	require.NoError(t, err)

	expectedAmount := big.NewInt(0).Sub(before.Stakes, amount)
	require.Equal(t, expectedAmount.String(), after.Stakes.String())
	println(fmt.Sprintf("testWithdraw - available node - index:%v node:%v owner:%v stakes:%v", expectedNewIndex, after.NodeAddress.Hex(), after.Owner.Hex(), after.Stakes.String()))
}

func testGetNodeAddressFromAddress(t *testing.T, masterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB) {
	method := "getNodeAddressFromOwner"
	var (
		input, output []byte
		err error
	)
	for _, n := range genesisNodes {
		owner := common.HexToAddress(n["owner"].(string))
		expectedNodeAddress := common.HexToAddress(n["address"].(string))
		input, err = masterAbi.Pack(method, owner)
		require.NoError(t, err)
		output, err = staticCall(owner, masterAddress, bc.CurrentHeader(), bc, input, st)
		require.NoError(t, err)
		type nAddress struct {
			Node common.Address `abi:"node"`
		}
		var na nAddress
		err = masterAbi.Unpack(&na, method, output)
		require.NoError(t, err)
		require.Equal(t, expectedNodeAddress.Hex(), na.Node.Hex())
	}
}

func testSetReward(t *testing.T, masterAbi abi.ABI, nodeAddress common.Address, blockHeight uint64, bc *blockchain.BlockChain, st *state.StateDB) {
	setRewarded := "setRewarded"
	getValidatedBlockHeightByIndex := "getValidatedBlockHeightByIndex"
	getNumberOfValidatedBlocks := "getNumberOfValidatedBlocks"
	var (
		nodeABI abi.ABI
		input, output []byte
		err error
		height uint64
		length *big.Int
	)
	println(fmt.Sprintf("testSetRewarded with node:%v", nodeAddress.Hex()))
	input, err = masterAbi.Pack(setRewarded, nodeAddress, blockHeight)
	require.NoError(t, err)
	_, err = call(posHandlerAddress, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)

	nodeABI, err = abi.JSON(strings.NewReader(NodeAbi))
	require.NoError(t, err)

	input, err = nodeABI.Pack(getNumberOfValidatedBlocks)
	require.NoError(t, err)
	output, err = staticCall(posHandlerAddress, nodeAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)
	err = nodeABI.Unpack(&length, getNumberOfValidatedBlocks, output)
	require.NoError(t, err)
	require.Equal(t, "1", length.String())

	input, err = nodeABI.Pack(getValidatedBlockHeightByIndex, uint64(0))
	require.NoError(t, err)
	output, err = staticCall(posHandlerAddress, nodeAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)
	err = nodeABI.Unpack(&height, getValidatedBlockHeightByIndex, output)
	require.NoError(t, err)
	println(height)
}

func testRejectBlock(t *testing.T, masterAbi abi.ABI, nodeAddress, sender common.Address, index int64, blockHeight uint64, bc *blockchain.BlockChain, st *state.StateDB) {

	type rejectedStatus struct {
		TotalVoted uint64 `abi:"totalVoted"`
		Status bool `abi:"status"`
	}

	var (
		input, output []byte
		err error
		nodeAbi abi.ABI
		hasVoted bool
		status rejectedStatus
		height uint64
	)
	getRejectedBlockHeightByIndex := "getRejectedBlockHeightByIndex"
	getRejectedStatus := "getRejectedStatus"
	hasRejectedVote := "hasRejectedVote"
	rejectBlockValidation := "rejectBlockValidation"

	nodeAbi, err = abi.JSON(strings.NewReader(NodeAbi))
	require.NoError(t, err)

	// add reject request from sender
	input, err = masterAbi.Pack(rejectBlockValidation, nodeAddress, blockHeight)
	require.NoError(t, err)

	output, err = call(sender, masterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err)

	// test if sender has voted
	input, err = masterAbi.Pack(hasRejectedVote, nodeAddress, blockHeight)
	require.NoError(t, err)

	output, err = staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	err = masterAbi.Unpack(&hasVoted, hasRejectedVote, output)
	require.NoError(t, err)
	require.Equal(t, true, hasVoted)

	// get rejected status
	input, err = masterAbi.Pack(getRejectedStatus, nodeAddress, blockHeight)
	require.NoError(t, err)
	output, err = staticCall(sender, masterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)

	err = masterAbi.Unpack(&status, getRejectedStatus, output)
	require.NoError(t, err)

	println(fmt.Sprintf("rejectedStatus: total:%v status:%v", status.TotalVoted, status.Status))
	if index == -1 {
		require.Equal(t, false, status.Status)
	}
	if index > -1 {
		require.Equal(t, true, status.Status)

		// getRejectedBlockHeightByIndex
		input, err = nodeAbi.Pack(getRejectedBlockHeightByIndex, uint64(index))
		require.NoError(t, err)
		output, err = staticCall(sender, nodeAddress, bc.CurrentHeader(), bc, input, st)
		require.NoError(t, err)
		require.NoError(t, nodeAbi.Unpack(&height, getRejectedBlockHeightByIndex, output))
		require.Equal(t, blockHeight, height)
	}
}

func testCollectDualValidators(t *testing.T, dualMasterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB) {
	// in order to collect validators each genesis need to request collect validators until 2/3+1 number of validators or genesis (if validators list is empty)
	type (
		validatorsInfo struct {
			TotalNodes uint64 `abi:"totalNodes"`
			StartAtBlock uint64 `abi:"startAtBlock"`
			EndAtBlock uint64 `abi:"endAtBlock"`
		}
		validatorInfo struct {
			Node common.Address `abi:"node"`
			Owner common.Address `abi:"owner"`
			Stakes *big.Int      `abi:"stakes"`
		}
	)

	var (
		input, output []byte
		err error
		valsInfo validatorsInfo
		valInfo validatorInfo
	)

	requestCollectValidators := "requestCollectValidators"
	getLatestValidatorsInfo := "getLatestValidatorsInfo"
	getLatestValidatorByIndex := "getLatestValidatorByIndex"

	input, err = dualMasterAbi.Pack(requestCollectValidators)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[0]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	input, err = dualMasterAbi.Pack(requestCollectValidators)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[1]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	input, err = dualMasterAbi.Pack(requestCollectValidators)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[2]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	println("---- test get overall validators information")

	input, err = dualMasterAbi.Pack(getLatestValidatorsInfo)
	require.NoError(t, err)
	output, err = staticCall(common.HexToAddress(genesisNodes[0]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err)
	err = dualMasterAbi.Unpack(&valsInfo, getLatestValidatorsInfo, output)
	require.NoError(t, err)
	require.Equal(t, uint64(1), valsInfo.StartAtBlock)
	require.Equal(t, uint64(3), valsInfo.TotalNodes)

	println("---- test get validator information by its index")
	input, err = dualMasterAbi.Pack(getLatestValidatorByIndex, uint64(1))
	require.NoError(t, err)
	output, err = staticCall(common.HexToAddress(genesisNodes[0]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err, string(output))
	err = dualMasterAbi.Unpack(&valInfo, getLatestValidatorByIndex, output)
	require.NoError(t, err)
	println(fmt.Sprintf("------ Index:%v address:%v owner:%v stakes:%v", 1, valInfo.Node.Hex(), valInfo.Owner.Hex(), valInfo.Stakes.String()))
	require.Equal(t, genesisNodes[0]["address"].(string), valInfo.Node.Hex())
	require.Equal(t, genesisNodes[0]["owner"].(string), valInfo.Owner.Hex())

	input, err = dualMasterAbi.Pack(getLatestValidatorByIndex, uint64(2))
	require.NoError(t, err)
	output, err = staticCall(common.HexToAddress(genesisNodes[0]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err, string(output))
	err = dualMasterAbi.Unpack(&valInfo, getLatestValidatorByIndex, output)
	require.NoError(t, err)
	println(fmt.Sprintf("------ Index:%v address:%v owner:%v stakes:%v", 2, valInfo.Node.Hex(), valInfo.Owner.Hex(), valInfo.Stakes.String()))
	require.Equal(t, genesisNodes[1]["address"].(string), valInfo.Node.Hex())
	require.Equal(t, genesisNodes[1]["owner"].(string), valInfo.Owner.Hex())

	input, err = dualMasterAbi.Pack(getLatestValidatorByIndex, uint64(3))
	require.NoError(t, err)
	output, err = staticCall(common.HexToAddress(genesisNodes[0]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, st)
	require.NoError(t, err, string(output))
	err = dualMasterAbi.Unpack(&valInfo, getLatestValidatorByIndex, output)
	require.NoError(t, err)
	println(fmt.Sprintf("------ Index:%v address:%v owner:%v stakes:%v", 3, valInfo.Node.Hex(), valInfo.Owner.Hex(), valInfo.Stakes.String()))
	require.Equal(t, genesisNodes[2]["address"].(string), valInfo.Node.Hex())
	require.Equal(t, genesisNodes[2]["owner"].(string), valInfo.Owner.Hex())
}

func testClaimDualReward(t *testing.T, dualMasterAbi abi.ABI, bc *blockchain.BlockChain, st *state.StateDB) {
	claimedNode := common.HexToAddress(genesisNodes[0]["address"].(string))
	blockHeight := uint64(1)
	requestClaimReward := "requestClaimReward"

	var (
		input, output []byte
		err error
	)

	nodeBalance := st.GetBalance(claimedNode)
	println(fmt.Sprintf("---- claiming reward for node:%v blockHeight:%v", claimedNode.Hex(), blockHeight))
	println(fmt.Sprintf("---- balance of node:%v before claiming is %v", claimedNode.Hex(), nodeBalance.String()))

	input, err = dualMasterAbi.Pack(requestClaimReward, claimedNode, blockHeight)
	require.NoError(t, err)
	output, err = call(claimedNode, dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	input, err = dualMasterAbi.Pack(requestClaimReward, claimedNode, blockHeight)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[1]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	input, err = dualMasterAbi.Pack(requestClaimReward, claimedNode, blockHeight)
	require.NoError(t, err)
	output, err = call(common.HexToAddress(genesisNodes[2]["owner"].(string)), dualMasterAddress, bc.CurrentHeader(), bc, input, big.NewInt(0), st)
	require.NoError(t, err, string(output))

	nodeBalance = st.GetBalance(claimedNode)
	println(fmt.Sprintf("---- balance of node:%v after claiming is %v", claimedNode.Hex(), nodeBalance.String()))
}

func setup(t *testing.T) (*blockchain.BlockChain, abi.ABI, abi.ABI, *state.StateDB) {
	bc, err := setupBlockchain()
	require.NoError(t, err)

	bc.ConsensusInfo = pos.ConsensusInfo{
		Master: &pos.MasterInfo{
			Address: masterAddress,
			ABI: MasterAbi,
			Nodes: pos.Nodes{
				ABI: NodeAbi,
			},
			Stakers: pos.Stakers{
				ABI: StakerAbi,
			},
		},
		DualMaster: &pos.DualMasterInfo{
			ABI: DualMasterAbi,
			BlockReward: blockReward,
		},
	}

	// setup Master smc
	masterAbi, err := abi.JSON(strings.NewReader(MasterAbi))
	require.NoError(t, err)

	dualMasterAbi, err := abi.JSON(strings.NewReader(DualMasterAbi))
	require.NoError(t, err)

	st, err := bc.State()
	require.NoError(t, err)

	return bc, masterAbi, dualMasterAbi, st
}

func TestMaster(t *testing.T) {
	bc, masterAbi, dualMasterAbi, st := setup(t)
	testCreateMaster(t, masterAbi, bc, st, uint64(10), uint64(4), uint64(50))
	testDeployNodesAndStakes(t, bc, st, genesisNodes, true)
	testGetTotalStakes(t, masterAbi, bc, st, minimumStakes)
	testCollectValidators(t, masterAbi, bc, st)
	testGetLatestValidators(t, masterAbi, bc, st, uint64(3), genesisNodes)
	testDeployNodesAndStakes(t, bc, st, normalNodes, false)
	testAddPendingNode(t, masterAbi, bc, st, normalNodes[0], common.HexToAddress(genesisNodes[0]["owner"].(string)))
	testGetPendingNode(t, masterAbi, bc, st, 1, common.HexToAddress(normalNodes[0]["address"].(string)), uint64(1))
	testVotePending(t, masterAbi, bc, st, []map[string]interface{}{genesisNodes[1]}, uint64(len(genesisNodes)))
	testVotePending(t, masterAbi, bc, st, []map[string]interface{}{genesisNodes[2]}, uint64(len(genesisNodes) + 1))
	testGetPendingNode(t, masterAbi, bc, st, 1, common.HexToAddress(normalNodes[0]["address"].(string)), uint64(3))
	testStake(t, bc, st, normalNodes[0], nil, minimumStakes, minimumStakes)
	testCollectValidators(t, masterAbi, bc, st)
	testGetLatestValidators(t, masterAbi, bc, st, uint64(4), append(genesisNodes, normalNodes[0]))

	// stakes to genesis[0] from recently added node.
	target := common.HexToAddress(genesisNodes[1]["address"].(string))
	testStake(t, bc, st, normalNodes[0], &target, minimumStakes, minimumStakes)
	testCollectValidators(t, masterAbi, bc, st)
	expectedNode := genesisNodes[1]
	expectedNode["expectedStakes"] = big.NewInt(0).Add(minimumStakes, minimumStakes)
	expectedNode["expectedStaker"] = uint64(3)
	testGetLatestValidators(t, masterAbi, bc, st, uint64(4), []map[string]interface{}{expectedNode, genesisNodes[0], genesisNodes[2], normalNodes[0]})

	// add the last node to pending
	println("add the last node to pending")
	testAddPendingNode(t, masterAbi, bc, st, normalNodes[1], common.HexToAddress(normalNodes[0]["owner"].(string)))
	testHasPendingVoted(t, masterAbi, bc, st, common.HexToAddress(genesisNodes[0]["owner"].(string)), uint64(2), false)
	testGetPendingNode(t, masterAbi, bc, st, 2, common.HexToAddress(normalNodes[1]["address"].(string)), uint64(1))
	testVotePending(t, masterAbi, bc, st, []map[string]interface{}{genesisNodes[0]}, uint64(4))
	testGetPendingNode(t, masterAbi, bc, st, 2, common.HexToAddress(normalNodes[1]["address"].(string)), uint64(2))
	testVotePending(t, masterAbi, bc, st, genesisNodes, uint64(5))

	// test delete latest node
	testRequestDelete(t, masterAbi, bc, st, 5, common.HexToAddress(normalNodes[0]["owner"].(string)), common.HexToAddress(normalNodes[1]["address"].(string)))
	testVoteDeleteNode(t, masterAbi, bc, st, 1, 5, 2, common.HexToAddress(genesisNodes[0]["owner"].(string)), common.HexToAddress(normalNodes[1]["address"].(string)))
	testVoteDeleteNode(t, masterAbi, bc, st, 1, 5, 3, common.HexToAddress(genesisNodes[1]["owner"].(string)), common.HexToAddress(normalNodes[1]["address"].(string)))
	testVoteDeleteNode(t, masterAbi, bc, st, 1, 5, 4, common.HexToAddress(genesisNodes[2]["owner"].(string)), common.HexToAddress(normalNodes[1]["address"].(string)))
	testAvailableNodes(t, masterAbi, bc, st, uint64(4))

	testAddPendingNode(t, masterAbi, bc, st, normalNodes[2], common.HexToAddress(normalNodes[0]["owner"].(string)))
	testHasPendingVoted(t, masterAbi, bc, st, common.HexToAddress(genesisNodes[0]["owner"].(string)), uint64(3), false)
	testGetPendingNode(t, masterAbi, bc, st, 3, common.HexToAddress(normalNodes[2]["address"].(string)), uint64(1))
	testVotePending(t, masterAbi, bc, st, []map[string]interface{}{genesisNodes[0]}, uint64(4))
	testGetPendingNode(t, masterAbi, bc, st, 3, common.HexToAddress(normalNodes[2]["address"].(string)), uint64(2))
	testVotePending(t, masterAbi, bc, st, genesisNodes, uint64(5))

	// test withdraw: assume staker withdraw an amount of KAI.
	withdraw, _ := big.NewInt(0).SetString("500000000000000000", 10)
	testGetAvailableNodeIndex(t, masterAbi, bc, st, common.HexToAddress(genesisNodes[0]["address"].(string)), uint64(2))
	testWithdraw(t, masterAbi, bc, st, common.HexToAddress(genesisNodes[0]["address"].(string)), common.HexToAddress(genesisNodes[0]["staker"].(string)), withdraw, 4)
	testAvailableNodes(t, masterAbi, bc, st, uint64(5))

	// test get node address from owner's address
	println("testGetNodeAddressFromAddress")
	testGetNodeAddressFromAddress(t, masterAbi, bc, st)
	testSetReward(t, masterAbi, common.HexToAddress(genesisNodes[0]["address"].(string)), 1, bc, st)

	// test sending rejected request
	println("test sending rejected request")
	rejectedAddress := common.HexToAddress(genesisNodes[0]["address"].(string))
	testRejectBlock(t, masterAbi, rejectedAddress, common.HexToAddress(genesisNodes[1]["owner"].(string)), -1, 1, bc, st)
	testRejectBlock(t, masterAbi, rejectedAddress, common.HexToAddress(genesisNodes[2]["owner"].(string)), -1, 1, bc, st)
	testRejectBlock(t, masterAbi, rejectedAddress, common.HexToAddress(normalNodes[0]["owner"].(string)), 0, 1, bc, st)

	println("test deploy and collect dual validators")
	testDeployDualMasterSmartContract(t, dualMasterAbi, bc, st, uint64(10), uint64(4), uint64(50))
	testCollectDualValidators(t, dualMasterAbi, bc, st)

	println("test claiming dual reward")
	testClaimDualReward(t, dualMasterAbi, bc, st)
}

func TestNode(t *testing.T) {
	kAbi, err := abi.JSON(strings.NewReader(NodeAbi))
	require.NoError(t, err)

	input, err := kAbi.Pack("",
		masterAddress,
		"7a86e2b7628c76fcae76a8b37025cba698a289a44102c5c021594b5c9fce33072ee7ef992f5e018dc44b98fa11fec53824d79015747e8ac474f4ee15b7fbe860",
		"node1",
		uint16(5),
		uint64(100),
		minimumStakes,
	)
	require.NoError(t, err)

	newCode := append(NodeByteCode, input...)
	bc, err := setupBlockchain()
	if err != nil {
		t.Fatal(err)
	}
	st, err := bc.State()
	if err != nil {
		t.Fatal(err)
	}

	address := common.HexToAddress("0x0000000000000000000000000000000000000010")

	// Setup contract code into genesis state
	_, contractAddr, _, err := create(common.HexToAddress("0xc1fe56E3F58D3244F606306611a5d10c8333f1f6"), address, bc.CurrentHeader(), bc, newCode, big.NewInt(0), st)
	require.NoError(t, err)
	require.Equal(t, address, *contractAddr)

	getOwner, err := kAbi.Pack("getOwner")
	require.NoError(t, err)

	result, err := staticCall(common.HexToAddress("0xc1fe56E3F58D3244F606306611a5d10c8333f1f6"), *contractAddr, bc.CurrentHeader(), bc, getOwner, st)
	require.NoError(t, err)

	// test get owner
	owner := common.BytesToAddress(result)
	require.Equal(t, "0xc1fe56E3F58D3244F606306611a5d10c8333f1f6", owner.Hex())

	// test get node info
	type nodeInfo struct {
		Owner common.Address `abi:"owner"`
		NodeId string `abi:"nodeId"`
		NodeName string `abi:"nodeName"`
		RewardPercentage uint16 `abi:"rewardPercentage"`
		Balance *big.Int `abi:"balance"`
	}
	getNodeInfo, err := kAbi.Pack("getNodeInfo")
	result, err = staticCall(common.HexToAddress("0xc1fe56E3F58D3244F606306611a5d10c8333f1f6"), *contractAddr, bc.CurrentHeader(), bc, getNodeInfo, st)
	require.NoError(t, err)

	var actualNodeInfo nodeInfo
	err = kAbi.Unpack(&actualNodeInfo, "getNodeInfo", result)
	require.NoError(t, err)

	expectedNodeInfo := &nodeInfo{
		Owner:            common.HexToAddress("0xc1fe56E3F58D3244F606306611a5d10c8333f1f6"),
		NodeId:           "7a86e2b7628c76fcae76a8b37025cba698a289a44102c5c021594b5c9fce33072ee7ef992f5e018dc44b98fa11fec53824d79015747e8ac474f4ee15b7fbe860",
		NodeName:         "node1",
		RewardPercentage: uint16(500),
		Balance:          big.NewInt(0),
	}
	require.Equal(t, expectedNodeInfo.Owner, actualNodeInfo.Owner)
	require.Equal(t, expectedNodeInfo.NodeId, actualNodeInfo.NodeId)
}
