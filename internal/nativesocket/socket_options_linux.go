//go:build linux

package nativesocket

import "syscall"

func configureNoSigpipe(_ int) error { return nil }

func socketSend(fd int, data []byte) (int, error) {
	return syscall.SendmsgN(fd, data, nil, nil, syscall.MSG_NOSIGNAL)
}

// socketWritable reports, without blocking, whether fd is writable (a nonblocking connect has finished or failed).
func socketWritable(fd int) (bool, error) {
	if fd < 0 || fd >= 1024 {
		return false, syscall.EINVAL
	}
	var set syscall.FdSet
	set.Bits[fd/64] |= 1 << (uint(fd) % 64)
	timeout := syscall.Timeval{}
	for {
		count, err := syscall.Select(fd+1, nil, &set, nil, &timeout)
		if err == syscall.EINTR {
			continue
		}
		return count > 0, err
	}
}
