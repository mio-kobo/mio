//go:build darwin

package nativesocket

import "syscall"

func configureNoSigpipe(fd int) error {
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_NOSIGPIPE, 1)
}

func socketSend(fd int, data []byte) (int, error) {
	return syscall.Write(fd, data)
}

// socketWritable reports, without blocking, whether fd is writable (a nonblocking connect has finished or failed).
func socketWritable(fd int) (bool, error) {
	if fd < 0 || fd >= 1024 {
		return false, syscall.EINVAL
	}
	var set syscall.FdSet
	set.Bits[fd/32] |= 1 << (uint(fd) % 32)
	timeout := syscall.Timeval{}
	for {
		err := syscall.Select(fd+1, nil, &set, nil, &timeout)
		if err == syscall.EINTR {
			continue
		}
		return err == nil && set.Bits[fd/32]&(1<<(uint(fd)%32)) != 0, err
	}
}
