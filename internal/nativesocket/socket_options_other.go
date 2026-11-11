//go:build !linux && !darwin && !windows

package nativesocket

import "syscall"

// Other Unix targets need their own MSG_NOSIGNAL/SO_NOSIGPIPE specialization.
func configureNoSigpipe(_ int) error { return nil }

func socketSend(fd int, data []byte) (int, error) {
	return syscall.Write(fd, data)
}

func socketWritable(_ int) (bool, error) { return false, syscall.ENOSYS }
