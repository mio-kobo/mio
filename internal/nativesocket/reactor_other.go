//go:build !linux && !windows

package nativesocket

import (
	"syscall"

	"Mio/mma"
)

// The reactor primitives exist for Linux only; elsewhere they fail closed with an "other" status.
func reactorOpen(call *mma.CallContext) error {
	return createSocket(call, &socketState{fd: -1, err: syscall.ENOSYS})
}
func reactorCtl(call *mma.CallContext) error    { return simpleStatus(call, 3, syscall.ENOSYS) }
func reactorWait(call *mma.CallContext) error   { return operationStatus(call, 4, 0, false, syscall.ENOSYS) }
func reactorSignal(call *mma.CallContext) error { return simpleStatus(call, 1, syscall.ENOSYS) }
func reactorAck(call *mma.CallContext) error    { return simpleStatus(call, 1, syscall.ENOSYS) }
