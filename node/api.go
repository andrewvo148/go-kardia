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

package node

import (
	"runtime"

	"github.com/kardiachain/go-kardia/lib/crypto"
	"github.com/kardiachain/go-kardia/lib/p2p"
)

// PublicNodeAPI offers helper utils
type PublicNodeAPI struct {
	node *Node
}

// NewPublicNodeAPI creates a new PublicNodeAPI instance
func NewPublicNodeAPI(node *Node) *PublicNodeAPI {
	return &PublicNodeAPI{node}
}

// PeersCount returns the number of peers that current node can connect to.
func (s *PublicNodeAPI) PeersCount() int {
	return s.node.server.PeerCount()
}

// Peers returns a list of peers with their information
func (s *PublicNodeAPI) Peers() []*p2p.PeerInfo {
	return s.node.server.PeersInfo()
}

// NodeName returns name of current node
func (s *PublicNodeAPI) NodeName() string {
	return s.node.config.Name
}

// NodeInfo represents a short summary of the information about a node
type NodeInfo struct {
	Name    string `json:"name"`    // Name of the node
	Address string `json:"address"` // Address of the node
	Enode   string `json:"enode"`   // Enode URL for adding this peer from remote peers
	IP      string `json:"ip"`      // IP address of the node
	Ports   struct {
		Discovery int `json:"discovery"` // UDP listening port for discovery protocol
		Listener  int `json:"listener"`  // TCP listening port for RLPx
	} `json:"ports"`
	ListenAddr string `json:"listenAddr"`
	OS         string `json:"os"`
	OSVer      string `json:"osVer"`
}

// NodeInfo returns infomation of current node
func (s *PublicNodeAPI) NodeInfo() *NodeInfo {
	info := &NodeInfo{
		Name:       s.node.server.NodeInfo().Name,
		Address:    crypto.PubkeyToAddress(s.node.config.NodeKey().PublicKey).Hex(),
		Enode:      s.node.server.NodeInfo().Enode,
		IP:         s.node.server.NodeInfo().IP,
		Ports:      s.node.server.NodeInfo().Ports,
		ListenAddr: s.node.server.NodeInfo().ListenAddr,
		OS:         runtime.GOOS,
		OSVer:      runtime.GOARCH,
	}

	return info
}
