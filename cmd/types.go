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

package main

type (
	Config struct {
		Node                 `yaml:"Node"`
		MainChain   *Chain   `yaml:"MainChain"`
		DualChain   *Chain   `yaml:"DualChain,omitempty"`
	}
	Node struct {
		P2P                        `yaml:"P2P"`
		LogLevel          string   `yaml:"LogLevel"`
		Name              string   `yaml:"Name"`
		DataDir           string   `yaml:"DataDir"`
		HTTPHost          string   `yaml:"HTTPHost"`
		HTTPPort          int      `yaml:"HTTPPort"`
		HTTPModules       []string `yaml:"HTTPModules"`
		HTTPVirtualHosts  []string `yaml:"HTTPVirtualHosts"`
		HTTPCors          []string `yaml:"HTTPCors"`
	}
	P2P struct {
		PrivateKey    string    `yaml:"PrivateKey"`
		ListenAddress string    `yaml:"ListenAddress"`
		MaxPeers      int       `yaml:"MaxPeers"`
		Address       string    `yaml:"Address"`
	}
	Chain struct {
		ServiceName   string         `yaml:"ServiceName"`
		Protocol      *string        `yaml:"Protocol,omitempty"`
		ChainID       uint64         `yaml:"ChainID"`
		NetworkID     uint64         `yaml:"NetworkID"`
		AcceptTxs     uint32         `yaml:"AcceptTxs"`
		ZeroFee       uint           `yaml:"ZeroFee"`
		IsDual        uint           `yaml:"IsDual"`
		Consensus     *Consensus     `yaml:"Consensus,omitempty"`
		DualConsensus *DualConsensus `yaml:"DualConsensus,omitempty"`
		Genesis       *Genesis       `yaml:"Genesis,omitempty"`
		TxPool        *Pool          `yaml:"TxPool,omitempty"`
		EventPool     *Pool          `yaml:"EventPool,omitempty"`
		Database      *Database      `yaml:"Database,omitempty"`
		Seeds         []string       `yaml:"Seeds"`
		Events        []Event 	     `yaml:"Events"`
		PublishedEndpoint  *string   `yaml:"PublishedEndpoint,omitempty"`
		SubscribedEndpoint *string   `yaml:"SubscribedEndpoint,omitempty"`
		Validators    []int          `yaml:"Validators,omitempty"`
		BaseAccount   BaseAccount    `yaml:"BaseAccount"`
	}
	Genesis struct {
		Addresses      []string      `yaml:"Addresses"`
		GenesisAmount  string        `yaml:"GenesisAmount"`
		Contracts      []Contract    `yaml:"Contracts"`
	}
	Consensus struct {
		Configuration               ConsensusConfiguration `yaml:"Configuration"`
		Compilation                Compilation       `yaml:"Compilation"`
		Deployment                 Deployment        `yaml:"Deployment"`
		DualGenesis                []DualGenesis     `yaml:"DualGenesis"` // DualGenesis defines all genesis dual master in order to make sure state of all chain remain the same.
	}
	ConsensusConfiguration struct {
		MaxViolatePercentageAllowed uint64           `yaml:"MaxViolatePercentageAllowed"`
		FetchNewValidatorsTime     uint64            `yaml:"FetchNewValidatorsTime"`
		MaxValidators              uint64            `yaml:"MaxValidators"`
		ConsensusPeriodInBlock     uint64            `yaml:"ConsensusPeriod"`
		BlockReward                string            `yaml:"BlockReward"`
		MinimumStakes              string            `yaml:"MinimumStakes"` // MinimumStakes defines the minimum amount that a user stakes to a node.
		LockedPeriod               uint64            `yaml:"LockedPeriod"`  // LockedPeriod defines the period in block that user cannot withdraw staked KAI.
		TimeoutPropose             uint64            `yaml:"TimeoutPropose"`
		TimeoutProposeDelta        uint64            `yaml:"TimeoutProposeDelta"`
		TimeoutPrevote             uint64            `yaml:"TimeoutPrevote"`
		TimeoutPrevoteDelta        uint64            `yaml:"TimeoutPrevoteDelta"`
		TimeoutPrecommit           uint64            `yaml:"TimeoutPrecommit"`
		TimeoutPrecommitDelta      uint64            `yaml:"TimeoutPrecommitDelta"`
		TimeoutCommit              uint64            `yaml:"TimeoutCommit"`
		SkipTimeoutCommit          bool              `yaml:"SkipTimeoutCommit"`
		CreateEmptyBlocks          bool              `yaml:"CreateEmptyBlocks"`
		CreateEmptyBlocksInterval  uint64            `yaml:"CreateEmptyBlocksInterval"`
		PeerGossipSleepDuration    uint64            `yaml:"PeerGossipSleepDuration"`
		PeerQueryMaj23SleepDuration uint64           `yaml:"PeerQueryMaj23SleepDuration"`
	}
	DualGenesis struct {
		Name                  string      `yaml:"Name"`
		ByteCode              string      `yaml:"ByteCode"`
		Address               string      `yaml:"Address"`
		GenesisNodes           []string   `yaml:"GenesisNodes"` // genesis nodes are nodes act as validator from the beginning when there aren't any other registered nodes.
	}
	DualConsensus struct {
		ABI          				string             `yaml:"ABI"`
		MasterAddress               string             `yaml:"MasterAddress"` // dual master address. Note: the address must be registered and added in Master smart contract.
		Configuration          ConsensusConfiguration  `yaml:"Configuration"`
	}
	Compilation struct { // Compilation contains compiled bytecodes and abi for Master.sol, Node.sol and Staker.sol
		Master     CompilationInfo  `yaml:"Master"`
		Staker     CompilationInfo  `yaml:"Staker"`
		Node       CompilationInfo  `yaml:"Node"`
	}
	CompilationInfo struct {
		ByteCode     string        `yaml:"ByteCode"`
		ABI          string        `yaml:"ABI"`
	}
	Deployment struct { // Deployment contains consensus genesis information that will be created at the beginning
		Master     MasterInfo    `yaml:"Master"`
		Stakers    []StakerInfo  `yaml:"Stakers"`
		Nodes      []NodeInfo    `yaml:"Nodes"`
	}
	MasterInfo struct { // MasterInfo contains master contract address and its genesis amount
		Address    string      `yaml:"Address"`
		GenesisAmount string   `yaml:"GenesisAmount"`
	}
	NodeInfo struct {
		Address             string       `yaml:"Address"`
		Owner               string       `yaml:"Owner"`
		PubKey              string       `yaml:"PubKey"`
		Name                string       `yaml:"Name"`
		RewardPercentage    uint16       `yaml:"RewardPercentage"`  // RewardPercentage defines total KAI that all stakers receive every validated block.
	}
	StakerInfo struct { // StakerInfo contains genesis staker address, node that it will stake to from the beginning and stakeAmount
		Address     string       `yaml:"Address"`       // Address is predefined staker's contract address
		Owner       string       `yaml:"Owner"`         // Owner is owner's address for staker contract address
		StakedNode  string       `yaml:"StakedNode"`    // StakedNode is genesis node's address that user will stake to
		StakeAmount string       `yaml:"StakeAmount"`
	}
	Contract struct {
		Address    string    `yaml:"Address,omitempty"`
		ByteCode   string    `yaml:"ByteCode"`
		ABI        string    `yaml:"ABI,omitempty"`
		GenesisAmount string `yaml:"GenesisAmount,omitempty"`
	}
	Pool struct {
		GlobalSlots  uint64 `yaml:"GlobalSlots"`
		GlobalQueue  uint64 `yaml:"GlobalQueue"`
		LifeTime     int    `yaml:"LifeTime"`
		AccountSlots uint64 `yaml:"AccountSlots"`
		AccountQueue uint64 `yaml:"AccountQueue"`
	}
	Database struct {
		Type         uint      `yaml:"Type"`
		Dir          string    `yaml:"Dir"`
		Caches       int       `yaml:"Caches"`
		Handles      int       `yaml:"Handles"`
		URI          string    `yaml:"URI"`
		Name         string    `yaml:"Name"`
		Drop         int       `yaml:"Drop"`
	}
	Event struct {
		MasterSmartContract string           `yaml:"MasterSmartContract"`
		ContractAddress   string             `yaml:"ContractAddress"`
		MasterABI         *string            `yaml:"MasterABI"`
		ABI               *string            `yaml:"ABI,omitempty"`
		Watchers          []Watcher          `yaml:"Watchers"`
	}
	Watcher struct {
		Method           string             `yaml:"Method"`
		WatcherActions   []string           `yaml:"WatcherActions,omitempty"`
		DualActions      []string           `yaml:"DualActions"`
	}
	BaseAccount struct {
		Address      string       `yaml:"Address"`
		PrivateKey   string       `yaml:"PrivateKey"`
	}
)
