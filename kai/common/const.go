package common

// Constants to match up protocol versions and messages
const (
	kai1 = 1
)

// ProtocolName is the official short name of the protocol used during capability negotiation.
var ProtocolName = "kai"

// ProtocolVersions are the supported versions of the eth protocol (first is primary).
var ProtocolVersions = []uint{kai1}

// ProtocolLengths are the number of implemented message corresponding to different protocol versions.
var ProtocolLengths = []uint64{8}

const ProtocolMaxMsgSize = 10 * 1024 * 1024 // Maximum cap on the size of a protocol message

// kai protocol message codes
const (
	// Protocol messages belonging to kai1
	StatusMsg         = 0x00
	TxMsg             = 0x01
	CsNewRoundStepMsg = 0x02 // Consensus message
	CsProposalMsg     = 0x03 // Proposal message
	CsVoteMsg         = 0x04 // Vote message
	CsCommitStepMsg   = 0x05 // Commit step message
	CsHasVoteMsg      = 0x06 // Has vote message
	CsProposalPOLMsg  = 0x07 // Proposal message
	// TODO(namdoh): Consider if this is needed.
	//CsBlockMsg = 0x08 // Block message
)
