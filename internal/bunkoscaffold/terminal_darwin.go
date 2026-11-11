package bunkoscaffold

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

func terminalIO(file *os.File, request uintptr, data unsafe.Pointer) bool {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(data))
	return err == 0
}
func terminalInput(file *os.File) bool {
	var state syscall.Termios
	return terminalIO(file, syscall.TIOCGETA, unsafe.Pointer(&state))
}
func terminalBytes(file *os.File) (func(), bool) {
	var saved syscall.Termios
	if !terminalIO(file, syscall.TIOCGETA, unsafe.Pointer(&saved)) {
		return nil, false
	}
	raw := saved
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ECHONL | syscall.ISIG | syscall.IEXTEN
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if !terminalIO(file, syscall.TIOCSETA, unsafe.Pointer(&raw)) {
		return nil, false
	}
	return func() { terminalIO(file, syscall.TIOCSETA, unsafe.Pointer(&saved)) }, true
}
func terminalPending(file *os.File) bool {
	time.Sleep(40 * time.Millisecond)
	var count int32
	return terminalIO(file, 0x4004667f, unsafe.Pointer(&count)) && count > 0
}
