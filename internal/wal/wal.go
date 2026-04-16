// [magic: 4B][opcode: 1B][key_len: 4B][val_len: 4B][key: key_len B][val: val_len B][checksum: 4B]
// magic : 0xDEADBEEF[used for fast scanning to find record boundaries]
// opcode : OpSet (0x01) or OpDelete (0x02)
// checksum : CRC32 (IEEE) over opcode + key_len + val_len + key + val
package wal

// opcode to identify the type of wal entry
type Opcode uint8

const (
	OpSet    Opcode = 0x01
	OpDelete Opcode = 0x02
)

// magic is written at the start of every entry to allow fast boundary scanning
const magic uint32 = 0xDEADBEEF

// headerSize = magic(4B) + opcode(1B) + key_len(4B) + val_len(4B)
const headerSize = 13

