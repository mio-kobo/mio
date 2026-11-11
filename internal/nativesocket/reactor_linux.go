//go:build linux

package nativesocket

import (
	"syscall"

	"Mio/mma"
)

const (
	reactorMaxEvents = 64
	eventfdFlags     = 0x80800 // EFD_CLOEXEC | EFD_NONBLOCK
)

// reactorOpen creates an epoll instance (kind 0) or an eventfd used to wake one (kind 1). The result is an ordinary
// socket-table handle, so socket.close releases it and a stale handle reports Closed.
func reactorOpen(call *mma.CallContext) error {
	kind, err := intArgument(call, 0)
	if err != nil {
		return err
	}
	fd := -1
	switch kind {
	case 0:
		fd, err = syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	case 1:
		raw, _, errno := syscall.Syscall(syscall.SYS_EVENTFD2, 0, eventfdFlags, 0)
		if errno != 0 {
			err = errno
		} else {
			fd = int(raw)
		}
	default:
		err = syscall.EINVAL
	}
	if err != nil {
		fd = -1
	}
	return createSocket(call, &socketState{fd: fd, err: err})
}

// reactorCtl: packed = op | interest<<8 | token<<16 (op 1 add, 2 delete, 3 modify; interest bit 0 readable, bit 1 writable).
func reactorCtl(call *mma.CallContext) error {
	packed, err := intArgument(call, 2)
	if err != nil {
		return err
	}
	var reactor, target *socketState
	reactor, err = useSocket(call)
	if err == nil {
		var value any
		value, err = call.UseResource(1, resourceType)
		if err == nil {
			target = value.(*socketState)
			if target.err != nil {
				err = target.err
			} else if target.fd < 0 {
				err = syscall.EBADF
			}
		}
	}
	if err == nil {
		op, interest, token := packed&0xff, (packed>>8)&0xff, packed>>16
		if packed < 0 || op < 1 || op > 3 || interest > 3 {
			err = syscall.EINVAL
		} else {
			var events uint32
			if interest&1 != 0 {
				events |= syscall.EPOLLIN
			}
			if interest&2 != 0 {
				events |= syscall.EPOLLOUT
			}
			event := syscall.EpollEvent{Events: events, Fd: int32(token), Pad: int32(token >> 32)}
			err = syscall.EpollCtl(reactor.fd, int(op), target.fd, &event)
		}
	}
	return simpleStatus(call, 3, err)
}

// reactorWait fills events with 16-byte records (token int64, mask int64: 1 readable, 2 writable, 4 error, 8 hangup) and
// reports the count in the status array. timeout_ms: -1 waits, 0 polls. Interruption by a signal reports zero events.
func reactorWait(call *mma.CallContext) error {
	maxEvents, err := intArgument(call, 2)
	if err != nil {
		return err
	}
	timeout, err := intArgument(call, 3)
	if err != nil {
		return err
	}
	state, err := useSocket(call)
	if err != nil {
		return operationStatus(call, 4, 0, false, err)
	}
	if maxEvents < 1 || maxEvents > reactorMaxEvents || timeout < -1 || timeout > 1<<31-1 {
		return operationStatus(call, 4, 0, false, syscall.EINVAL)
	}
	if _, err = call.ReadBytes(1, 0, 16*maxEvents); err != nil {
		return operationStatus(call, 4, 0, false, syscall.EINVAL)
	}
	var raw [reactorMaxEvents]syscall.EpollEvent
	count, err := syscall.EpollWait(state.fd, raw[:maxEvents], int(timeout))
	if err == syscall.EINTR {
		count, err = 0, nil
	}
	if err != nil {
		return operationStatus(call, 4, 0, false, err)
	}
	if count > 0 {
		out := make([]byte, 16*count)
		for i := 0; i < count; i++ {
			token := uint64(uint32(raw[i].Fd)) | uint64(uint32(raw[i].Pad))<<32
			var mask uint64
			if raw[i].Events&syscall.EPOLLIN != 0 {
				mask |= 1
			}
			if raw[i].Events&syscall.EPOLLOUT != 0 {
				mask |= 2
			}
			if raw[i].Events&syscall.EPOLLERR != 0 {
				mask |= 4
			}
			if raw[i].Events&syscall.EPOLLHUP != 0 {
				mask |= 8
			}
			for b := 0; b < 8; b++ {
				out[16*i+b] = byte(token >> (8 * b))
				out[16*i+8+b] = byte(mask >> (8 * b))
			}
		}
		if err = call.WriteBytes(1, 0, out); err != nil {
			return operationStatus(call, 4, 0, false, syscall.EINVAL)
		}
	}
	return operationStatus(call, 4, int64(count), false, nil)
}

// reactorSignal adds one to the eventfd counter (a full counter means a wake is already pending: success).
func reactorSignal(call *mma.CallContext) error {
	state, err := useSocket(call)
	if err == nil {
		_, err = syscall.Write(state.fd, []byte{1, 0, 0, 0, 0, 0, 0, 0})
		if err == syscall.EAGAIN {
			err = nil
		}
	}
	return simpleStatus(call, 1, err)
}

// reactorAck resets the eventfd counter (nothing pending is success).
func reactorAck(call *mma.CallContext) error {
	state, err := useSocket(call)
	if err == nil {
		var buffer [8]byte
		_, err = syscall.Read(state.fd, buffer[:])
		if err == syscall.EAGAIN {
			err = nil
		}
	}
	return simpleStatus(call, 1, err)
}
