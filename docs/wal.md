# the wal: the durability contract

The Write-Ahead Log (WAL) is the foundation of durability in RaftKV. If it's not on disk, it's not real. I used a custom binary format and strict `fsync` to guarantee that data survives a crash.

---

## strict durability: fsync

I call `file.Sync()` on every write. This is the only way to ensure the data is physically on the disk platter rather than sitting in the OS page cache. It adds latency, but it satisfies the durability contract.

---

## binary wire format

The WAL uses a 16-byte header for every entry. It's a raw binary format designed for alignment and safety.

| field | size | description |
| :--- | :--- | :--- |
| **magic** | 4B | `0xDEADBEEF`. Used to detect the start of a valid record. |
| **key_len** | 4B | Length of the key in bytes. |
| **val_len** | 4B | Length of the value in bytes. |
| **opcode** | 1B | Operation type (SET or DELETE). |
| **CRC32** | 4B | IEEE CRC32 checksum covering the header and payload. |

---

## crash recovery

On startup, the node replays the WAL from start to finish to rebuild the in-memory map.

### handling torn writes
If the power fails mid-write, you get a "torn write"—a corrupted or truncated entry at the end of the file. 
The recovery logic detects this via the magic number and CRC32 checks. If a corruption is found at the end of the log, the store **truncates** the file to remove the garbage, ensuring a clean state for the next run.

---

## append-only design
I never modify existing entries in the WAL. All operations, including deletions, are appended to the end. This is faster for I/O and simplifies the recovery logic. The latest record for a key in the log is the only one that matters.
