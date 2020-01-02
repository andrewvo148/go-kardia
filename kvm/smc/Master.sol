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

pragma solidity ^0.5.8;


/**
 * Master is used to stores nodes info including available and pending nodes, stakers.
 **/
contract Master {

    string constant methodUpdateBlock = "updateBlock(uint64,bool)";
    string constant methodIsViolatedNode = "isViolatedNode(address,uint64)";
    string constant getOwner = "getOwner()";
    string constant updateStakeAmount = "updateStakeAmount(address,uint256)";
    string constant joinDualFunc = "join(address,address,uint256)";
    address constant PoSHandler = 0x0000000000000000000000000000000000000005;

    address[] _genesisNodes = [
    0x0000000000000000000000000000000000000010,
    0x0000000000000000000000000000000000000011,
    0x0000000000000000000000000000000000000012
    ];

    address[] _genesisOwners = [
    0xc1fe56E3F58D3244F606306611a5d10c8333f1f6,
    0x7cefC13B6E2aedEeDFB7Cb6c32457240746BAEe5,
    0xfF3dac4f04dDbD24dE5D6039F90596F0a8bb08fd
    ];

    address[] _genesisStakers = [
    0x0000000000000000000000000000000000000020,
    0x0000000000000000000000000000000000000021,
    0x0000000000000000000000000000000000000022
    ];

    address[] _availableDualNodes = [
    0x0000000000000000000000000000000000000000,
    0x0000000000000000000000000000000000000030,
    0x0000000000000000000000000000000000000040,
    0x0000000000000000000000000000000000000050
    ];


    mapping(address=>bool) _isGenesis;
    mapping(address=>bool) _isGenesisOwner;

    modifier isGenesis {
        require(isMasterGenesis(msg.sender), "user does not have genesis permission");
        _;
    }

    modifier _isPoSHandler {
        require(msg.sender == PoSHandler, "sender is not PoSHandler");
        _;
    }

    modifier _isValidatorOrGenesis {
        require(isMasterGenesis(msg.sender) || isValidator(msg.sender), "sender is neither validator and genesis");
        _;
    }

    modifier _isAvailableNodes {
        require(isAvailableNodes(msg.sender) > 0, "sender does not belong in any availableNodes");
        _;
    }

    modifier _isStaker {
        require(isStaker(msg.sender), "user is not staker");
        _;
    }

    struct StakerInfo {
        address staker;
        uint256 amount;
    }

    struct NodeInfo {
        address node;
        address owner;
        uint256 stakes;
        uint64 totalStaker;
        uint64 dualIndex; // if a node is dual node its index is greater than 0.
    }

    struct NodeIndex {
        mapping(uint64=>StakerInfo) stakerInfo;
        mapping(address=>uint64) stakerAdded;
    }

    struct PendingInfo {
        NodeInfo node;
        uint64 vote;
        mapping(address=>bool) votedAddress;
        bool done;
    }

    struct PendingDeleteInfo {
        NodeInfo node;
        uint64 index;
        uint64 vote;
        mapping(address=>bool) votedAddress;
        bool done;
    }

    struct Validators {
        uint64 totalNodes;
        uint64 startAtBlock;
        uint64 endAtBlock;
        mapping(uint64=>NodeInfo) nodes;
        mapping(address=>uint64) addedNodes;
    }

    struct RejectedVotes {
        uint64 totalVoted;
        bool status;
        mapping(address=>bool) voted;
    }

    // _rejectedVote contains info of voting process of rejecting validation of a node for a specific block.
    // the first key is rejected block height, the second key is node's address.
    mapping(uint64=>mapping(address=>RejectedVotes)) _rejectedVotes;

    // _history contains all validators through period.
    Validators[] _history;

    // availableNodes is used to mark all nodes passed the voting process or genesisNodes
    // use index as an index to make it easy to handle with update/remove list since it's a bit complicated handling list in solidity.
    NodeInfo[] _availableNodes;
    mapping(address=>uint) _availableAdded;
    mapping(address=>NodeIndex) _nodeIndex;
    mapping(address=>address) _ownerNode;

    // pendingNodes is a map contains all pendingNodes that are added by availableNodes and are waiting for +2/3 vote.
    PendingInfo[] _pendingNodes;
    mapping(address=>uint) _pendingAdded;

    // _startAtBlock stores started block in every consensusPeriod
    uint64 _startAtBlock = 0;

    // _nextBlock stores started block for the next consensusPeriod
    uint64 _nextBlock = 0;

    // _consensusPeriod indicates the period a consensus has.
    uint64 _consensusPeriod;

    uint64 _maxValidators;
    uint64 _maxViolatePercentage;

    PendingDeleteInfo[] _pendingDeletedNodes;
    mapping(address=>uint) _deletingAdded;

    // _stakers is used to check if an address is a staker smart contract or not
    mapping(address=>bool) _stakers;

    // _stakerFromOwners is used to get staker's smart contract from its owner.
    mapping(address=>address) _stakerFromOwners;

    mapping(address=>mapping(uint64=>bool)) _rewarded;

    constructor(uint64 consensusPeriod, uint64 maxValidators, uint64 maxViolatePercentage) public {
        _consensusPeriod = consensusPeriod;
        _maxValidators = maxValidators;
        _maxViolatePercentage = maxViolatePercentage;

        _availableNodes.push(NodeInfo(address(0x0), address(0x0), 0, 0, 0));
        _pendingNodes.push(PendingInfo(_availableNodes[0], 0, true));
        _pendingDeletedNodes.push(PendingDeleteInfo(_availableNodes[0], 0, 0, true));

        for (uint i=0; i < _genesisNodes.length; i++) {
            address genesisAddress = _genesisNodes[i];
            _isGenesis[genesisAddress] = true;
            _isGenesisOwner[_genesisOwners[i]] = true;
            _ownerNode[_genesisOwners[i]] = genesisAddress;

            _availableNodes.push(NodeInfo(genesisAddress, _genesisOwners[i], 0, 1, 0));
            _availableAdded[genesisAddress] = i+1;
            _nodeIndex[genesisAddress].stakerInfo[0] = StakerInfo(address(0x0), 0);
        }
    }

    function getAddressOwner(address addr) internal view returns (address) {
        (bool success, bytes memory result) = addr.staticcall(abi.encodeWithSignature(getOwner));
        require(success, "fail to getOwner");
        return abi.decode(result, (address));
    }

    // addNode adds a node to pending list
    function addPendingNode(address nodeAddress) public _isAvailableNodes {
        if (_pendingAdded[nodeAddress] == 0) {
            address owner = getAddressOwner(nodeAddress);
            _pendingNodes.push(PendingInfo(NodeInfo(nodeAddress, owner, 0, 1, 0), 1, false));
            _pendingNodes[_pendingNodes.length-1].votedAddress[msg.sender] = true;
            _pendingAdded[nodeAddress] = _pendingNodes.length-1;
        }
    }

    function getPendingNode(uint64 index) public view returns (address nodeAddress, uint256 stakes, uint64 vote) {
        PendingInfo storage info = _pendingNodes[index];
        return (info.node.node, info.node.stakes, info.vote);
    }

    function getTotalPending() public view returns (uint) {
        return _pendingNodes.length - 1;
    }

    function getTotalAvailableNodes() public view returns (uint) {
        return _availableNodes.length - 1;
    }

    function GetTotalPendingDelete() public view returns (uint) {
        return _pendingDeletedNodes.length - 1;
    }

    function getAvailableNodeInfo(address node) public view returns (address nodeAddress, address owner, uint64 dualIndex, uint256 stakes, uint64 totalStaker) {
        uint index = getAvailableNodeIndex(node);
        return getAvailableNode(index);
    }

    function getAvailableNode(uint index) public view returns (address nodeAddress, address owner, uint64 dualIndex, uint256 stakes, uint64 totalStaker) {
        require(index > 0 && index < _availableNodes.length, "getAvailableNode:invalid index");
        NodeInfo storage info = _availableNodes[index];
        return (info.node, info.owner, info.dualIndex, info.stakes, info.totalStaker);
    }

    function getStakerInfo(address node, uint64 index) public view returns (address staker, uint256 amount) {
        (,,,,uint64 totalStaker) = getAvailableNode(_availableAdded[node]);
        require(index > 0 && index < totalStaker, "getStakerInfo:invalid index");
        return (_nodeIndex[node].stakerInfo[index].staker, _nodeIndex[node].stakerInfo[index].amount);
    }

    function getAvailableNodeIndex(address node) public view returns (uint index) {
        return _availableAdded[node];
    }

    // votePending is used when a valid user (belongs to availableNodes) vote for a node.
    function votePending(uint64 index) public _isAvailableNodes {
        require(index > 0 && index < _pendingNodes.length, "invalid index");
        if (!_pendingNodes[index].votedAddress[msg.sender]) {
            _pendingNodes[index].vote += 1;
            _pendingNodes[index].votedAddress[msg.sender] = true;

            // if vote >= 2/3 _totalAvailableNodes then update node to availableNodes
            if (isQualified(_pendingNodes[index].vote)) {
                updatePending(index);
            }
        }
    }

    // requestDelete requests delete an availableNode based on its index in _availableNodes.
    function requestDelete(uint64 index) public _isAvailableNodes {
        require(index > 0 && index < _availableNodes.length, "invalid index");
        // get node from availableNodes
        NodeInfo storage node = _availableNodes[index];

        // if node is not genesis
        if (!_isGenesis[node.node] && _deletingAdded[node.node] == 0) {
            _pendingDeletedNodes.push(PendingDeleteInfo(node, index, 1, false));
            _pendingDeletedNodes[_pendingDeletedNodes.length-1].votedAddress[msg.sender] = true;
            _deletingAdded[node.node] = _pendingDeletedNodes.length-1;
        }
    }

    function getRequestDeleteNode(uint64 index) public view returns (uint64 nodeIndex, address nodeAddress, uint256 stakes, uint64 vote) {
        PendingDeleteInfo storage info = _pendingDeletedNodes[index];
        return (info.index, info.node.node, info.node.stakes, info.vote);
    }

    // voteDeleting votes to delete an availableNode based on index in _pendingDeletedNodes
    function voteDeleting(uint64 index) public _isAvailableNodes {
        require(index > 0 && index < _pendingDeletedNodes.length, "invalid index");
        PendingDeleteInfo storage info = _pendingDeletedNodes[index];
        if (!info.votedAddress[msg.sender]) {
            info.vote += 1;
            info.votedAddress[msg.sender] = true;
            _pendingDeletedNodes[index] = info;

            // if vote >= 2/3 _totalAvailableNodes then update node to availableNodes
            if (isQualified(info.vote)) {
                updateDeletePending(index);
            }
        }
    }

    // isQualified checks if vote count is greater than or equal with 2/3 total or not.
    function isQualified(uint64 count) internal view returns (bool) {
        return count >= ((_availableNodes.length-1)*2/3) + 1;
    }

    // updatePending updates pending node into _availableNodes
    function updatePending(uint64 index) internal {
        require(index > 0 && index < _pendingNodes.length, "invalid index");
        // get pending info at index
        PendingInfo storage info = _pendingNodes[index];
        _pendingNodes[index].done = true;
        if (_availableAdded[info.node.node] > 0) return;
        // append pending info to availableNodes
        _availableNodes.push(info.node);
        _availableAdded[info.node.node] = _availableNodes.length-1;
        _nodeIndex[info.node.node].stakerInfo[0] = StakerInfo(address(0x0), 0);
    }

    function hasPendingVoted(uint64 index) public view returns (bool) {
        return _pendingNodes[index].votedAddress[msg.sender];
    }

    function deleteAvailableNode(uint64 index) internal {
        require(index > 0 && index < _availableNodes.length, "invalid index");

        // get node info from index
        NodeInfo storage nodeInfo = _availableNodes[index];

        // update _availableAdded to false
        _availableAdded[nodeInfo.node] = 0;

        while (index < _availableNodes.length - 1) { // index is not last element
            NodeInfo storage nextNode = _availableNodes[index + 1];
            _availableNodes[index] = nextNode;
            _availableAdded[nextNode.node] = index;
            index += 1;
        }

        // remove index - now is the last element
        delete _availableNodes[index];
        _availableNodes.length--;
    }

    function updateDeletePending(uint64 index) internal {
        require(index > 0 && index < _pendingDeletedNodes.length, "invalid index");
        // get delete pending info at index
        PendingDeleteInfo storage info = _pendingDeletedNodes[index];

        // delete availableNodes
        deleteAvailableNode(info.index);

        // update _deletingAdded to false
        _deletingAdded[info.node.node] = 0;
        _pendingDeletedNodes[index].done = true;
    }

    function changeConsensusPeriod(uint64 consensusPeriod) public isGenesis {
        _consensusPeriod = consensusPeriod;
    }

    // migrateBalance is used for migrating to new version of Master, if there is any new update needed in the future.
    function migrateBalance(address payable newMasterVersion) public isGenesis {
        newMasterVersion.transfer(address(this).balance);
    }

    // addNode is used after posHandler creates new node's smart contract.
    // then it will call this function to save returned address.
    function addNode(address node) public _isPoSHandler {
        address owner = getAddressOwner(node);
        _ownerNode[owner] = node;
    }

    // addStaker is used after posHandler creates new staker smart contract.
    // then it will call this function to save returned address.
    function addStaker(address staker) public _isPoSHandler {
        _stakers[staker] = true;
        address owner = getAddressOwner(staker);
        _stakerFromOwners[owner] = staker;
    }

    // stake is called by using delegateCall in staker contract address. therefore msg.sender is staker's contract address
    function stake(address nodeAddress, uint256 amount) public _isStaker {
        require(amount > 0, "invalid amount");

        uint index = _availableAdded[nodeAddress];
        if (index > 0) { // sender must be owner of the node.
            // update stakes
            _availableNodes[index].stakes += amount;

            // add staker to stake info if it does not exist, otherwise update stakerInfo
            if (_nodeIndex[nodeAddress].stakerAdded[msg.sender] > 0) {
                uint64 stakerIndex = _nodeIndex[nodeAddress].stakerAdded[msg.sender];
                // update StakeInfo
                _nodeIndex[nodeAddress].stakerInfo[stakerIndex].amount += amount;
            } else { // staker does not exist
                uint64 newIndex = _availableNodes[index].totalStaker;
                _nodeIndex[nodeAddress].stakerAdded[msg.sender] = newIndex;
                _nodeIndex[nodeAddress].stakerInfo[newIndex] = StakerInfo(msg.sender, amount);
                _availableNodes[index].totalStaker += 1;
            }

            // re-index node
            while(index > 1) {
                if (_availableNodes[index].stakes <= _availableNodes[index-1].stakes) break;
                // update _availableAdded
                _availableAdded[_availableNodes[index].node] = index-1;
                _availableAdded[_availableNodes[index-1].node] = index;

                // switch node
                NodeInfo memory temp = _availableNodes[index-1];
                _availableNodes[index-1] = _availableNodes[index];
                _availableNodes[index] = temp;
                index -= 1;
            }

            // update stake amount in dual Master
            updateDualStakeAmount(index);
        }
    }

    // withdraw: after user chooses withdraw, staker's contract will call this function to update node's stakes
    function withdraw(address nodeAddress, uint256 amount) public _isStaker {
        uint index = _availableAdded[nodeAddress];
        require(index > 0 && _nodeIndex[nodeAddress].stakerAdded[msg.sender] > 0, "invalid index");

        uint64 stakerIndex = _nodeIndex[nodeAddress].stakerAdded[msg.sender];

        // update total stakes.
        _availableNodes[index].stakes -= amount;

        // update staker's stakes
        _nodeIndex[nodeAddress].stakerInfo[stakerIndex].amount -= amount;

        // re-index node.
        while (index < _availableNodes.length-1) {
            if (_availableNodes[index].stakes > _availableNodes[index+1].stakes) break;
            // update _availableAdded
            _availableAdded[_availableNodes[index].node] = index+1;
            _availableAdded[_availableNodes[index+1].node] = index;

            // switch node
            NodeInfo memory temp = _availableNodes[index+1];
            _availableNodes[index+1] = _availableNodes[index];
            _availableNodes[index] = temp;
            index++;
        }
        // update stake amount in dual Master
        updateDualStakeAmount(index);
    }

    // updateDualStakeAmount updates stake amount in dual address for a specific node, after a staker stakes/withdraws KAI into/from it.
    function updateDualStakeAmount(uint index) internal {
        if (_availableNodes[index].dualIndex > 0) {
            address dualMaster = _availableDualNodes[_availableNodes[index].dualIndex];
            (bool success, ) = dualMaster.call(abi.encodeWithSignature(updateStakeAmount, _availableNodes[index].node, _availableNodes[index].stakes));
            require(success == true, "update stake amount fail");
        }
    }

    // collectValidators base on available nodes, max validators, collect validators and start new consensus period.
    // sometime, tx may be delayed for some blocks due to the traffic.
    // before adding new period, update last end block with current blockHeight
    // update _startAtBlock with current blockHeight + 1

    function collectValidators() public _isValidatorOrGenesis {
        // update _startAtBlock and _nextBlock
        _startAtBlock = _nextBlock;
        _nextBlock += _consensusPeriod+1;

        _history.push(Validators(1, _startAtBlock, _nextBlock-1));
        _history[_history.length-1].nodes[0] = _availableNodes[0];

        // get len based on _totalAvailableNodes and _maxValidators
        uint len = _availableNodes.length-1;
        if (len > _maxValidators) len = _maxValidators;
        // check valid nodes.
        for (uint64 i=1; i <= len; i++) {
            if (_availableNodes[i].stakes == 0) continue;
            uint64 currentIndex = _history[_history.length-1].totalNodes;
            _history[_history.length-1].nodes[currentIndex] = _availableNodes[i];
            _history[_history.length-1].addedNodes[_availableNodes[i].node] = currentIndex;
            _history[_history.length-1].totalNodes += 1;
        }
    }

    function getTotalStakes(address node) public view returns (uint256) {
        uint index = _availableAdded[node];
        if (index > 0) {
            return _availableNodes[index].stakes;
        }
        return 0;
    }

    function isStaker(address staker) public view returns (bool) {
        return _stakers[staker];
    }

    // isAvailableNodes check whether an address belongs to any available node or not.
    function isAvailableNodes(address node) public view returns (uint64) {
        uint total = getTotalAvailableNodes();
        if (total == 0) return 0; // total available is empty
        for (uint64 i=1; i<=total; i++) {
            if (_availableNodes[i].node == node || _availableNodes[i].owner == node) {
                return i;
            }
        }
        return 0;
    }

    // isValidator checks an address whether it belongs into latest validator.
    function isValidator(address sender) public view returns (bool) {
        if (_history.length == 0) return false;
        Validators memory validators = _history[_history.length-1];
        for (uint64 i=1; i < validators.totalNodes; i++) {
            address owner = _history[_history.length-1].nodes[i].owner;
            address node = _history[_history.length-1].nodes[i].node;
            if (owner == sender || node == sender) {
                return true;
            }
        }
        return false;
    }

    function getLatestValidatorsInfo() public view returns (uint64 totalNodes, uint64 startAtBlock, uint64 endAtBlock) {
        if (_history.length == 0) return (0, 0, 0);
        return (_history[_history.length-1].totalNodes-1, _history[_history.length-1].startAtBlock, _history[_history.length-1].endAtBlock);
    }

    function getLatestValidatorByIndex(uint64 index) public view returns (address node, address owner, uint256 stakes, uint64 totalStaker) {
        (uint64 len, , ) = getLatestValidatorsInfo();
        require(index <= len, "invalid index");
        NodeInfo memory validator = _history[_history.length-1].nodes[index];
        return (validator.node, validator.owner, validator.stakes, validator.totalStaker);
    }

    // rejectBlockValidation requests reject validation of a node for given blockHeight, if total requests are greater than or equal 2/3 + 1
    // then update status of _rejectedVote and call smart contract function 'methodUpdateRejectedBlock' to targeted node to update the rejected block.
    function rejectBlockValidation(address node, uint64 blockHeight) public _isValidatorOrGenesis {
        uint64 index = isAvailableNodes(node);
        require(index > 0, "this node is not in availableNodes");
        if (!_rejectedVotes[blockHeight][node].voted[msg.sender]) { // sender has not voted yet.
            _rejectedVotes[blockHeight][node].voted[msg.sender] = true;
            _rejectedVotes[blockHeight][node].totalVoted += 1;
            (uint64 totalNodes,,) = getLatestValidatorsInfo();
            bool is23 = _rejectedVotes[blockHeight][node].totalVoted >= totalNodes*2/3 + 1;
            if (is23 && !_rejectedVotes[blockHeight][node].status) {
                // if totalVoted >= 2/3 + 1 and voting process has not done yet, update status and call smc function on node's address
                // to update rejected block
                _rejectedVotes[blockHeight][node].status = true;
                (bool success, bytes memory result) = node.call(abi.encodeWithSignature(methodUpdateBlock, blockHeight, true));
                require(success, "updateBlock failed");

                // check if node is violated or not by calling vaildate function to posHandler.
                // if it is request delete the node.
                (success, result) = PoSHandler.staticcall(abi.encodeWithSignature(methodIsViolatedNode, node, _maxViolatePercentage));
                require(success, "call check violated node to posHandler failed");
                bool isViolated = abi.decode(result, (bool));
                if (isViolated) {
                    requestDelete(index);
                }
            }
        }
    }

    function hasRejectedVote(address node, uint64 blockHeight) public view returns (bool) {
        return _rejectedVotes[blockHeight][node].voted[msg.sender];
    }

    function getRejectedStatus(address node, uint64 blockHeight) public view returns (uint64 totalVoted, bool status) {
        return (_rejectedVotes[blockHeight][node].totalVoted, _rejectedVotes[blockHeight][node].status);
    }

    // isRewarded is used to validate if a node was rewarded in given blockHeight or not.
    function isRewarded(address node, uint64 blockHeight) public view returns (bool) {
        return _rewarded[node][blockHeight];
    }

    // setRewarded marks that a node has been rewarded at given blockHeight.
    function setRewarded(address node, uint64 blockHeight) public _isPoSHandler returns (bool success, bytes memory result) {
        _rewarded[node][blockHeight] = true;
        return node.call(abi.encodeWithSignature(methodUpdateBlock, blockHeight, false));
    }

    function getNodeAddressFromOwner(address owner) public view returns (address node) {
        return _ownerNode[owner];
    }

    function isMasterGenesis(address sender) public view returns (bool) {
        return _isGenesis[sender] || _isGenesisOwner[sender];
    }

    function dualAddressIndex(address dualAddress) public view returns (uint64) {
        for (uint64 i=1; i < _availableDualNodes.length; i++) {
            if (_availableDualNodes[i] == dualAddress) {
                return i;
            }
        }
        return 0;
    }

    function joinDualNode(address dualAddress) public _isAvailableNodes {
        uint index = isAvailableNodes(msg.sender);
        uint64 dualIndex = dualAddressIndex(dualAddress);

        require(_availableNodes[index].dualIndex == 0 && dualIndex > 0, "this node has already joined another node");
        (bool success,) = dualAddress.call(abi.encodeWithSignature(joinDualFunc, _availableNodes[index].node, _availableNodes[index].owner, _availableNodes[index].stakes));
        require(success, "join dual address fail");

        // update dualIndex to node info
        _availableNodes[index].dualIndex = dualIndex;
    }
}
