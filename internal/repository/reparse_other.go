//go:build !windows

package repository

import (
	"os"
	"syscall"
)

func reparse(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }

func linkCount(file *os.File) (uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return uint64(info.Sys().(*syscall.Stat_t).Nlink), nil
}

func readFlags() int { return os.O_RDONLY | syscall.O_NONBLOCK }
