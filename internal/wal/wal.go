//header layout (16 bytes, all integers little-endian):
//[magic: 4B @ 0][key_len: 4B @ 4][val_len: 4B @ 8][opcode: 1B @ 12][version: 1B @ 13][reserved: 2B @ 14]
//
//magic    : 0xDEADBEEF[used for fast scanning to find record boundaries]
//opcode   : OpSet (0x01) or OpDelete (0x02)
//version  : currentVersion (0x01); bump when the format changes
//reserved : zeroed, available for future flags (at present not in use)
// checksum : CRC32(IEEE) over header bytes [4:16] + key + val
//
//failure safety:
// 1)crash during append i.e, entry is partially written; magic+checksum mismatch detected at replay and entry is skipped.
// 2)crash after wal write but before memory apply i.e, replay re-applies the entry safely (idempotent for SET; harmless re-delete for DELETE).
package wal

//opcode to identify the type of wal entry
type Opcode uint8

const (
	OpSet    Opcode = 0x01
	OpDelete Opcode = 0x02
)

const (
	magic          uint32 = 0xDEADBEEF
	currentVersion uint8  = 0x01

	//  headerSize = magic(4B) + key_len(4B) + val_len(4B) + opcode(1B) + version(1B) + reserved(2B)
	headerSize = 16
)

//WALHeader mirrors the on-disk header layout.
//4-byte fields first so every uint32 lands on a naturally aligned offset
//2 reserved bytes at the end are zeroed on write; available for future flags
type WALHeader struct {
	Magic    uint32 // offset 0
	KeyLen   uint32 // ofset 4
	ValLen   uint32 // offset 8
	Opcode   Opcode // offset 12
	Version  uint8  // offset 13
	_        [2]byte //offset 14-15 [reserved]
}
