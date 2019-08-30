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

package types

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"math/big"

	"github.com/kardiachain/go-kardia/lib/common"
	"github.com/kardiachain/go-kardia/lib/crypto/sha3"
	amino "github.com/kardiachain/go-kardia/lib/go-amino"
	"github.com/kardiachain/go-kardia/lib/log"
	"github.com/kardiachain/go-kardia/lib/merkle"
	"github.com/kardiachain/go-kardia/lib/rlp"
	"github.com/kardiachain/go-kardia/lib/trie"
)

const (
	MaxLimitBlockStore = 200     // Not use yet
	MaxBlockBytes      = 1048510 // lMB
	// BlockPartSizeBytes is the size of one block part.
	BlockPartSizeBytes = 65536 // 64kB
)

var (
	//ErrPartSetUnexpectedIndex is Error part set unexpected index
	ErrPartSetUnexpectedIndex = errors.New("error part set unexpected index")
	//ErrPartSetInvalidProof is Error part set invalid proof
	ErrPartSetInvalidProof = errors.New("error part set invalid proof")

	EmptyRootHash = DeriveSha(Transactions{})
	cdc           = amino.NewCodec()
)

//go:generate gencodec -type Header -field-override headerMarshaling -out gen_header_json.go

// Header represents a block header in the Kardia blockchain.
type Header struct {
	// basic block info
	Height uint64   `json:"height"       gencodec:"required"`
	Time   *big.Int `json:"time"         gencodec:"required"` // TODO(thientn/namdoh): epoch seconds, change to milis.
	NumTxs uint64   `json:"num_txs"      gencodec:"required"`
	// TODO(namdoh@): Create a separate block type for Dual's blockchain.
	NumDualEvents uint64 `json:"num_dual_events" gencodec:"required"`

	GasLimit uint64 `json:"gasLimit"         gencodec:"required"`
	GasUsed  uint64 `json:"gasUsed"          gencodec:"required"`

	// prev block info
	LastBlockID BlockID `json:"last_block_id"`
	//@huny TotalTxs    uint64   `json:"total_txs"`

	Coinbase common.Address `json:"miner"            gencodec:"required"`

	// hashes of block data
	LastCommitHash common.Hash `json:"last_commit_hash"    gencodec:"required"` // commit from validators from the last block
	TxHash         common.Hash `json:"data_hash"           gencodec:"required"` // transactions
	// TODO(namdoh@): Create a separate block type for Dual's blockchain.
	DualEventsHash common.Hash `json:"dual_events_hash"    gencodec:"required"` // dual's events
	Root           common.Hash `json:"stateRoot"           gencodec:"required"` // state root
	ReceiptHash    common.Hash `json:"receiptsRoot"        gencodec:"required"` // receipt root
	Bloom          Bloom       `json:"logsBloom"           gencodec:"required"`

	// hashes from the app output from the prev block
	ValidatorsHash common.Hash `json:"validators_hash"` // validators for the current block
	ConsensusHash  common.Hash `json:"consensus_hash"`  // consensus params for current block
	//@huny AppHash         common.Hash `json:"app_hash"`          // state after txs from the previous block
	//@huny LastResultsHash common.Hash `json:"last_results_hash"` // root hash of all results from the txs from the previous block

	// consensus info
	//@huny EvidenceHash common.Hash `json:"evidence_hash"` // evidence included in the block
}

// Hash returns the block hash of the header, which is simply the keccak256 hash of its
// RLP encoding.
func (h *Header) Hash() common.Hash {
	return rlpHash(h)
}

// Size returns the approximate memory used by all internal contents. It is used
// to approximate and limit the memory consumption of various caches.
func (h *Header) Size() common.StorageSize {
	return common.StorageSize(unsafe.Sizeof(*h))
}

// StringLong returns a long string representing full info about Header
func (h *Header) StringLong() string {
	if h == nil {
		return "nil-Header"
	}
	// TODO(thientn): check why String() of common.Hash is not called when logging, and have to call Hex() instead.
	return fmt.Sprintf("Header{Height:%v  Time:%v  NumTxs:%v  LastBlockID:%v  LastCommitHash:%v  TxHash:%v  Root:%v  ValidatorsHash:%v  ConsensusHash:%v}#%v",
		h.Height, time.Unix(h.Time.Int64(), 0), h.NumTxs, h.LastBlockID, h.LastCommitHash.Hex(), h.TxHash.Hex(), h.Root.Hex(), h.ValidatorsHash.Hex(), h.ConsensusHash.Hex(), h.Hash().Hex())

}

// String returns a short string representing Header by simplifying byte array to hex, and truncate the first 12 character of hex string
func (h *Header) String() string {
	if h == nil {
		return "nil-Header"
	}
	headerHash := h.Hash()
	return fmt.Sprintf("Header{Height:%v  Time:%v  NumTxs:%v  LastBlockID:%v  LastCommitHash:%v  TxHash:%v  Root:%v  ValidatorsHash:%v  ConsensusHash:%v}#%v",
		h.Height, time.Unix(h.Time.Int64(), 0), h.NumTxs, h.LastBlockID, h.LastCommitHash.Fingerprint(),
		h.TxHash.Fingerprint(), h.Root.Fingerprint(), h.ValidatorsHash.Fingerprint(), h.ConsensusHash.Fingerprint(), headerHash.Fingerprint())
}

// Body is a simple (mutable, non-safe) data container for storing and moving
// a block's data contents together.
type Body struct {
	Transactions []*Transaction
	DualEvents   []*DualEvent
	LastCommit   *Commit
}

func (b *Body) Copy() *Body {
	var bodyCopy Body
	bodyCopy.LastCommit = b.LastCommit.Copy()
	bodyCopy.Transactions = make([]*Transaction, len(b.Transactions))
	copy(bodyCopy.Transactions, b.Transactions)
	bodyCopy.DualEvents = make([]*DualEvent, len(b.DualEvents))
	copy(bodyCopy.DualEvents, b.DualEvents)
	return &bodyCopy
}

// Body returns the non-header content of the block.
func (b *Block) Body() *Body {
	return &Body{Transactions: b.transactions, DualEvents: b.dualEvents, LastCommit: b.lastCommit}
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

// BlockAccount stores basic data of an account in block.
type BlockAccount struct {
	// Cannot use map because of RLP.
	Addr    *common.Address
	Balance *big.Int
}

// Block represents an entire block in the Kardia blockchain.
type Block struct {
	logger log.Logger

	mtx          sync.Mutex
	header       *Header
	transactions Transactions
	dualEvents   DualEvents
	lastCommit   *Commit

	// caches
	hash atomic.Value
	size atomic.Value
}

// "external" block encoding. used for Kardia protocol, etc.
type extblock struct {
	Header     *Header
	Txs        []*Transaction
	DualEvents []*DualEvent
	LastCommit *Commit
}

// NewBlock creates a new block. The input data is copied,
// changes to header and to the field values will not affect the
// block.
//
// The values of TxHash and NumTxs in header are ignored and set to values
// derived from the given txs.
func NewBlock(logger log.Logger, header *Header, txs []*Transaction, receipts []*Receipt, commit *Commit) *Block {
	b := &Block{
		logger:     logger,
		header:     CopyHeader(header),
		lastCommit: CopyCommit(commit),
	}

	if len(txs) == 0 {
		b.header.TxHash = EmptyRootHash
	} else {
		b.header.TxHash = DeriveSha(Transactions(txs))
		b.header.NumTxs = uint64(len(txs))
		b.transactions = make(Transactions, len(txs))
		copy(b.transactions, txs)
	}

	if len(receipts) == 0 {
		b.header.ReceiptHash = EmptyRootHash
	} else {
		b.header.ReceiptHash = DeriveSha(Receipts(receipts))
		b.header.Bloom = CreateBloom(receipts)
	}

	if b.header.LastCommitHash.IsZero() {
		if commit == nil {
			b.logger.Error("NewBlock - commit should never be nil.")
			b.header.LastCommitHash = common.NewZeroHash()
		} else {
			b.logger.Trace("Compute last commit hash", "commit", commit)
			b.header.LastCommitHash = commit.Hash()
		}
	}

	// TODO(namdoh): Store evidence hash.

	return b
}

// NewDualBlock creates a new block for dual chain. The input data is copied,
// changes to header and to the field values will not affect the
// block.
func NewDualBlock(logger log.Logger, header *Header, events DualEvents, commit *Commit) *Block {
	b := &Block{
		logger:     logger,
		header:     CopyHeader(header),
		lastCommit: CopyCommit(commit),
	}

	b.header.DualEventsHash = EmptyRootHash

	if b.header.LastCommitHash.IsZero() {
		if commit == nil {
			b.logger.Error("NewBlock - commit should never be nil.")
			b.header.LastCommitHash = common.NewZeroHash()
		} else {
			b.logger.Trace("Compute last commit hash", "commit", commit)
			b.header.LastCommitHash = commit.Hash()
		}
	}

	if len(events) == 0 {
		b.header.DualEventsHash = EmptyRootHash
	} else {
		b.header.DualEventsHash = DeriveSha(DualEvents(events))
		b.header.NumDualEvents = uint64(len(events))
		b.dualEvents = make(DualEvents, len(events))
		copy(b.dualEvents, events)
	}

	// TODO(namdoh): Store evidence hash.

	return b
}

func (b *Block) SetLogger(logger log.Logger) {
	b.logger = logger
}

// NewBlockWithHeader creates a block with the given header data. The
// header data is copied, changes to header and to the field values
// will not affect the block.
func NewBlockWithHeader(logger log.Logger, header *Header) *Block {
	return &Block{header: CopyHeader(header)}
}

// CopyHeader creates a deep copy of a block header to prevent side effects from
// modifying a header variable.
func CopyHeader(h *Header) *Header {
	cpy := *h
	return &cpy
}

// CopyHeader creates a deep copy of a block commit to prevent side effects from
// modifying a commit variable.
func CopyCommit(c *Commit) *Commit {
	if c == nil {
		return c
	}
	cpy := *c
	return &cpy
}

//  DecodeRLP implements rlp.Decoder, decodes RLP stream to Block struct.
func (b *Block) DecodeRLP(s *rlp.Stream) error {
	var eb extblock
	_, size, _ := s.Kind()
	if err := s.Decode(&eb); err != nil {
		return err
	}
	// TODO(namdo,issues#73): Remove this hack, which address one of RLP's diosyncrasies.
	eb.LastCommit.MakeEmptyNil()

	b.header, b.transactions, b.dualEvents, b.lastCommit = eb.Header, eb.Txs, eb.DualEvents, eb.LastCommit
	b.size.Store(common.StorageSize(rlp.ListSize(size)))
	return nil
}

// EncodeRLP serializes Block into the RLP stream.
func (b *Block) EncodeRLP(w io.Writer) error {
	// TODO(namdo,issues#73): Remove this hack, which address one of RLP's diosyncrasies.
	lastCommitCopy := b.lastCommit.Copy()
	lastCommitCopy.MakeNilEmpty()
	return rlp.Encode(w, extblock{
		Header:     b.header,
		Txs:        b.transactions,
		DualEvents: b.dualEvents,
		LastCommit: lastCommitCopy,
	})
}

//  DecodeRLP implements rlp.Decoder, decodes RLP stream to Body struct.
// Custom Encode/Decode for Body because of LastCommit RLP issue#73, otherwise Body can use RLP default decoder.
func (b *Body) DecodeRLP(s *rlp.Stream) error {
	var eb extblock
	if err := s.Decode(&eb); err != nil {
		return err
	}
	// TODO(namdo,issues#73): Remove this hack, which address one of RLP's diosyncrasies.
	eb.LastCommit.MakeEmptyNil()

	b.Transactions, b.DualEvents, b.LastCommit = eb.Txs, eb.DualEvents, eb.LastCommit
	return nil
}

func (b *Body) EncodeRLP(w io.Writer) error {
	lastCommitCopy := b.LastCommit.Copy()
	lastCommitCopy.MakeNilEmpty()
	return rlp.Encode(w, extblock{
		Header:     &Header{},
		Txs:        b.Transactions,
		DualEvents: b.DualEvents,
		LastCommit: lastCommitCopy,
	})
}

func (b *Block) Transactions() Transactions { return b.transactions }

func (b *Block) Transaction(hash common.Hash) *Transaction {
	for _, transaction := range b.transactions {
		if transaction.Hash() == hash {
			return transaction
		}
	}
	return nil
}

func (b *Block) DualEvents() DualEvents { return b.dualEvents }

// WithBody returns a new block with the given transaction.
func (b *Block) WithBody(body *Body) *Block {
	block := &Block{
		logger:       b.logger,
		header:       CopyHeader(b.header),
		transactions: make([]*Transaction, len(body.Transactions)),
		dualEvents:   make([]*DualEvent, len(body.DualEvents)),
		lastCommit:   body.LastCommit,
	}
	copy(block.transactions, body.Transactions)
	copy(block.dualEvents, body.DualEvents)
	return block
}

func (b *Block) Height() uint64   { return b.header.Height }
func (b *Block) GasLimit() uint64 { return b.header.GasLimit }
func (b *Block) GasUsed() uint64  { return b.header.GasUsed }
func (b *Block) Time() *big.Int   { return b.header.Time }
func (b *Block) NumTxs() uint64   { return b.header.NumTxs }

func (b *Block) LastCommitHash() common.Hash { return b.header.LastCommitHash }
func (b *Block) TxHash() common.Hash         { return b.header.TxHash }
func (b *Block) Root() common.Hash           { return b.header.Root }
func (b *Block) ReceiptHash() common.Hash    { return b.header.ReceiptHash }
func (b *Block) Bloom() Bloom                { return b.header.Bloom }
func (b *Block) LastCommit() *Commit         { return b.lastCommit }

// TODO(namdoh): This is a hack due to rlp nature of decode both nil or empty
// struct pointer as nil. After encoding an empty struct and send it over to
// another node, decoding it would become nil.
func (b *Block) SetLastCommit(c *Commit) {
	b.logger.Error("SetLastCommit is a hack. Remove asap!!")
	b.lastCommit = c
}

func (b *Block) Header() *Header { return CopyHeader(b.header) }

func (b *Block) HashesTo(bid BlockID) bool {
	return b.Hash().Equal(bid.Hash)
}

// Size returns the true RLP encoded storage size of the block, either by encoding
// and returning it, or returning a previously cached value.
func (b *Block) Size() common.StorageSize {
	if size := b.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, b)
	b.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

// ValidateBasic performs basic validation that doesn't involve state data.
// It checks the internal consistency of the block.
func (b *Block) ValidateBasic() error {
	if b == nil {
		return errors.New("nil block")
	}
	b.mtx.Lock()
	defer b.mtx.Unlock()

	newTxs := uint64(len(b.transactions))
	if b.header.NumTxs != newTxs {
		return fmt.Errorf("Wrong Block Header/NumTxs. Expected %v, got %v", newTxs, b.header.NumTxs)
	}

	if b.lastCommit == nil && !b.header.LastCommitHash.IsZero() {
		return fmt.Errorf("Wrong Block.Header.LastCommitHash.  lastCommit is nil, but expect zero hash, but got: %v", b.header.LastCommitHash)
	} else if b.lastCommit != nil && !b.header.LastCommitHash.Equal(b.lastCommit.Hash()) {
		return fmt.Errorf("Wrong Block.Header.LastCommitHash.  Expected %v, got %v.  Last commit %v", b.header.LastCommitHash, b.lastCommit.Hash(), b.lastCommit)
	}
	if b.header.Height != 1 {
		if err := b.lastCommit.ValidateBasic(); err != nil {
			return err
		}
	}
	// TODO(namdoh): Re-enable check for Data hash.
	b.logger.Info("Block.ValidateBasic() - not yet implement validating data hash.")
	//if !bytes.Equal(b.DataHash, b.Data.Hash()) {
	//	return fmt.Errorf("Wrong Block.Header.DataHash.  Expected %v, got %v", b.DataHash, b.Data.Hash())
	//}
	//if !bytes.Equal(b.EvidenceHash, b.Evidence.Hash()) {
	//	return errors.New(cmn.Fmt("Wrong Block.Header.EvidenceHash.  Expected %v, got %v", b.EvidenceHash, b.Evidence.Hash()))
	//}

	b.logger.Info("Block.ValidateBasic() - implement validate DualEvents.")

	return nil
}

// StringLong returns a long string representing full info about Block
func (b *Block) StringLong() string {
	if b == nil {
		return "nil-Block"
	}

	return fmt.Sprintf("Block{%v  %v  %v  %v}#%v",
		b.header, b.transactions, b.dualEvents, b.lastCommit, b.Hash().Hex())
}

// String returns a short string representing block by simplifying block header and lastcommit
func (b *Block) String() string {
	if b == nil {
		return "nil-Block"
	}
	blockHash := b.Hash()
	return fmt.Sprintf("Block{h:%v  tx:%v  de:%v  c:%v}#%v",
		b.header, b.transactions, b.dualEvents, b.lastCommit, blockHash.Fingerprint())
}

type writeCounter common.StorageSize

func (c *writeCounter) Write(b []byte) (int, error) {
	*c += writeCounter(len(b))
	return len(b), nil
}

// Hash returns the keccak256 hash of b's header.
// The hash is computed on the first call and cached thereafter.
func (b *Block) Hash() common.Hash {
	if b == nil {
		log.Warn("Hashing nil block")
		return common.Hash{}
	}

	if hash := b.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	value := b.header.Hash()
	b.hash.Store(value)
	return value
}

// This function is used to address RLP's diosyncrasies (issues#73), enabling
// RLP encoding/decoding to pass.
// Note: Use this "before" sending the object to other peers.
func (b *Block) MakeNilEmpty() {
	if b.lastCommit != nil {
		b.lastCommit.MakeNilEmpty()
	}
}

// This function is used to address RLP's diosyncrasies (issues#73), enabling
// RLP encoding/decoding to pass.
// Note: Use this "after" receiving the object to other peers.
func (b *Block) MakeEmptyNil() {
	if b.lastCommit != nil {
		b.lastCommit.MakeEmptyNil()
	}
}

//Part struct
type Part struct {
	Index uint               `json:"index"`
	Bytes []byte             `json:"bytes"`
	Proof merkle.SimpleProof `json:"proof"`

	hash []byte
}

//Hash is part's hash
func (part *Part) Hash() []byte {
	if part.hash != nil {
		return part.hash
	}
	tmp := rlpHash(part.Bytes)
	part.hash = tmp[:]
	return part.hash
}

func (part *Part) String() string {
	return part.StringIndented("")
}

//StringIndented indent's string
func (part *Part) StringIndented(indent string) string {
	return fmt.Sprintf(`Part{#%v
%s  Bytes: %X...
%s  Proof: %v
%s}`,
		part.Index,
		indent, common.Fingerprint(part.Bytes),
		indent, "comment",
		// commented tmp by iceming
		// indent, part.Proof.StringIndented(indent+"  "),
		indent)
}

//PartSetHeader struct
type PartSetHeader struct {
	Total uint   `json:"total"`
	Hash  []byte `json:"hash"`
}

func (psh PartSetHeader) String() string {
	return fmt.Sprintf("%v:%X", psh.Total, common.Fingerprint(psh.Hash))
}

//IsZero is have part
func (psh PartSetHeader) IsZero() bool {
	return psh.Total == 0
}

//Equals compare other partSet header's hash
func (psh PartSetHeader) Equals(other PartSetHeader) bool {
	return psh.Total == other.Total && bytes.Equal(psh.Hash, other.Hash)
}

//PartSet struct
type PartSet struct {
	total uint
	hash  []byte

	mtx           sync.Mutex
	parts         []*Part
	partsBitArray *common.BitArray
	count         uint
}

// NewPartSetFromData Returns an immutable, full PartSet from the data bytes.
// The data bytes are split into "partSize" chunks, and merkle tree computed.
func NewPartSetFromData(data []byte, partSize uint) *PartSet {
	// divide data into 4kb parts.
	total := (len(data) + int(partSize) - 1) / int(partSize)
	parts := make([]*Part, total)
	partsBytes := make([][]byte, total)
	partsBitArray := common.NewBitArray(total)
	for i := 0; i < total; i++ {
		part := &Part{
			Index: uint(i),
			Bytes: data[i*int(partSize) : common.MinInt(len(data), (i+1)*int(partSize))],
		}
		parts[i] = part
		partsBytes[i] = part.Bytes
		partsBitArray.SetIndex(i, true)
	}
	// Compute merkle proofs
	root, proofs := merkle.SimpleProofsFromByteSlices(partsBytes)
	for i := 0; i < total; i++ {
		parts[i].Proof = *proofs[i]
	}
	return &PartSet{
		total:         uint(total),
		hash:          root,
		parts:         parts,
		partsBitArray: partsBitArray,
		count:         uint(total),
	}
}

// NewPartSetFromHeader Returns an empty PartSet ready to be populated.
func NewPartSetFromHeader(header PartSetHeader) *PartSet {
	return &PartSet{
		total:         header.Total,
		hash:          header.Hash,
		parts:         make([]*Part, header.Total),
		partsBitArray: common.NewBitArray(int(header.Total)),
		count:         0,
	}
}

//Header get partSet's header
func (ps *PartSet) Header() PartSetHeader {
	if ps == nil {
		return PartSetHeader{}
	}
	return PartSetHeader{
		Total: ps.total,
		Hash:  ps.hash,
	}
}

//HasHeader Compare header
func (ps *PartSet) HasHeader(header PartSetHeader) bool {
	if ps == nil {
		return false
	}
	return ps.Header().Equals(header)
}

//BitArray return BitArray's Copy
func (ps *PartSet) BitArray() *common.BitArray {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return ps.partsBitArray.Copy()
}

//Hash return ps'hash
func (ps *PartSet) Hash() []byte {
	if ps == nil {
		return nil
	}
	return ps.hash
}

//HashesTo Compare two hashes
func (ps *PartSet) HashesTo(hash []byte) bool {
	if ps == nil {
		return false
	}
	return bytes.Equal(ps.hash, hash)
}

//Count Count of parts
func (ps *PartSet) Count() uint {
	if ps == nil {
		return 0
	}
	return ps.count
}

//Total sum of parts
func (ps *PartSet) Total() uint {
	if ps == nil {
		return 0
	}
	return ps.total
}

//AddPart add a part to parts array
func (ps *PartSet) AddPart(part *Part) (bool, error) {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	// Invalid part index
	if part.Index >= ps.total {
		return false, ErrPartSetUnexpectedIndex
	}

	// If part already exists, return false.
	if ps.parts[part.Index] != nil {
		return false, nil
	}
	// Check hash proof
	if part.Proof.Verify(ps.Hash(), part.Hash()) != nil {
		return false, ErrPartSetInvalidProof
	}
	// Add part
	ps.parts[part.Index] = part
	ps.partsBitArray.SetIndex(int(part.Index), true)
	ps.count++
	return true, nil
}

//GetPart get a part for index
func (ps *PartSet) GetPart(index uint) *Part {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return ps.parts[index]
}

//IsComplete if get all part
func (ps *PartSet) IsComplete() bool {
	return ps.count == ps.total
}

//GetReader get a new reader if not complete
func (ps *PartSet) GetReader() io.Reader {
	if !ps.IsComplete() {
		common.PanicSanity("Cannot GetReader() on incomplete PartSet")
	}
	return NewPartSetReader(ps.parts)
}

//MakePartSet makes block to partset
func MakePartSet(partSize uint, block *Block) (*PartSet, error) {
	// Prefix the byte length, so that unmarshaling
	// can easily happen via a reader.
	bzs, err := rlp.EncodeToBytes(block)
	if err != nil {
		panic(err)
	}
	bz, err := cdc.MarshalBinary(bzs)
	if err != nil {
		return nil, err
	}
	return NewPartSetFromData(bz, partSize), nil
}

//MakeBlockFromPartSet makes partSet to block
func MakeBlockFromPartSet(reader *PartSet) (*Block, error) {
	if reader.IsComplete() {
		maxsize := int64(MaxBlockBytes)
		b := make([]byte, maxsize, maxsize)
		_, err := cdc.UnmarshalBinaryReader(reader.GetReader(), &b, maxsize)
		if err != nil {
			return nil, err
		}
		var block Block
		if err = rlp.DecodeBytes(b, &block); err != nil {
			return nil, err
		}
		return &block, nil
	}
	return nil, errors.New("Make block from partset not complete")
}

//PartSetReader struct
type PartSetReader struct {
	i      int
	parts  []*Part
	reader *bytes.Reader
}

//NewPartSetReader return new reader
func NewPartSetReader(parts []*Part) *PartSetReader {
	return &PartSetReader{
		i:      0,
		parts:  parts,
		reader: bytes.NewReader(parts[0].Bytes),
	}
}

//Read  read a partSet bytes
func (psr *PartSetReader) Read(p []byte) (n int, err error) {
	readerLen := psr.reader.Len()
	if readerLen >= len(p) {
		return psr.reader.Read(p)
	} else if readerLen > 0 {
		n1, err := psr.Read(p[:readerLen])
		if err != nil {
			return n1, err
		}
		n2, err := psr.Read(p[readerLen:])
		return n1 + n2, err
	}

	psr.i++
	if psr.i >= len(psr.parts) {
		return 0, io.EOF
	}
	psr.reader = bytes.NewReader(psr.parts[psr.i].Bytes)
	return psr.Read(p)
}

//StringShort print partSet count and total
func (ps *PartSet) StringShort() string {
	if ps == nil {
		return "nil-PartSet"
	}
	ps.mtx.Lock()
	defer ps.mtx.Unlock()
	return fmt.Sprintf("(%v of %v)", ps.Count(), ps.Total())
}

// BlockID struct
type BlockID struct {
	Hash        common.Hash   `json:"hash"`
	PartsHeader PartSetHeader `json:"parts"`
}

func NewZeroBlockID() BlockID {
	return BlockID{}
}

func (b *BlockID) IsZero() bool {
	return len(b.Hash) == 0
}

func (b BlockID) Equal(id BlockID) bool {
	return b.Hash.Equal(id.Hash)
}

// Key returns a machine-readable string representation of the BlockID
func (blockID *BlockID) Key() string {
	return string(blockID.Hash[:])
}

// String returns the first 12 characters of hex string representation of the BlockID
func (blockID BlockID) String() string {
	return common.Hash(blockID.Hash).Fingerprint()
}

func (blockID BlockID) StringLong() string {
	return common.Hash(blockID.Hash).Hex()
}

// BlockID return Hash of a block
func (b *Block) BlockHash() common.Hash {
	return b.Hash()
}

// BlockID return Hash of a block
func (b *Block) BlockID() BlockID {
	return BlockID{Hash: b.Hash()}
}

type Blocks []*Block

type BlockBy func(b1, b2 *Block) bool

func (self BlockBy) Sort(blocks Blocks) {
	bs := blockSorter{
		blocks: blocks,
		by:     self,
	}
	sort.Sort(bs)
}

type blockSorter struct {
	blocks Blocks
	by     func(b1, b2 *Block) bool
}

func (self blockSorter) Len() int { return len(self.blocks) }
func (self blockSorter) Swap(i, j int) {
	self.blocks[i], self.blocks[j] = self.blocks[j], self.blocks[i]
}
func (self blockSorter) Less(i, j int) bool { return self.by(self.blocks[i], self.blocks[j]) }

func Height(b1, b2 *Block) bool { return b1.header.Height < b2.header.Height }

// Helper function
type DerivableList interface {
	Len() int
	GetRlp(i int) []byte
}

func DeriveSha(list DerivableList) common.Hash {
	keybuf := new(bytes.Buffer)
	t := new(trie.Trie)
	for i := 0; i < list.Len(); i++ {
		keybuf.Reset()
		rlp.Encode(keybuf, uint(i))
		t.Update(keybuf.Bytes(), list.GetRlp(i))
	}
	return t.Hash()
}
