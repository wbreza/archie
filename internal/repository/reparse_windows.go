//go:build windows

package repository

import (
	"os"
	"syscall"
)

func reparse(info os.FileInfo) bool {
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}

	return info.Mode()&os.ModeSymlink != 0
}

func linkCount(file *os.File) (uint64, error) {
	var info syscall.ByHandleFileInformation
	err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &info)
	return uint64(info.NumberOfLinks), err
}

func readFlags() int { return os.O_RDONLY }
