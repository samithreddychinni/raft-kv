package store

import (
	"os"
)

func openReadOnly(path string) (*os.File, error) {
	return os.Open(path)
}

func currentOffset(f *os.File) (int64, error) {
	return f.Seek(0, os.SEEK_CUR)
}

func truncate(path string, size int64) error {
	return os.Truncate(path, size)
}
