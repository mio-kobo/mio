//go:build !windows

// Package nativesocket provides the VM backend for Mio's low-level socket
// primitives. Native AOT lowers the same symbols directly to OS syscalls.
package nativesocket

import (
	"encoding/binary"
	"errors"
	"io"
	"syscall"

	"Mio/mma"
)

const (
	modulePath    = "mio/sys/socket"
	resourceType  = "mio/sys/socket.Socket"
	maxNativeRead = 256 << 20
)

// The byte-sized codes are shared with the native AOT socket backend.
const (
	statusOK byte = iota
	statusInvalidArgument
	statusClosed
	statusTimedOut
	statusConnectionRefused
	statusConnectionReset
	statusAddressInUse
	statusPermissionDenied
	statusWouldBlock
	statusOther
	statusInProgress
)

type socketState struct {
	fd  int
	err error
}

// Register exposes OS primitives, never TCP buffering or connection policy.
func Register(registry *mma.ModuleRegistry) error {
	return registry.RegisterModule(mma.ModuleDefinition{Path: modulePath, Functions: []mma.FunctionDefinition{
		def("open", nil, mma.TypeResource, socketOpen),
		def("status", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, socketStatus),
		def("bind_ipv4", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, socketBind),
		def("connect_ipv4", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, socketConnect),
		def("listen", []mma.Type{mma.TypeResource, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, socketListen),
		def("accept", []mma.Type{mma.TypeResource}, mma.TypeResource, socketAccept),
		def("read", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, socketRead),
		def("write", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, socketWrite),
		def("close", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, socketClose),
		def("set_nonblocking", []mma.Type{mma.TypeResource, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, socketSetNonblocking),
		def("finish_connect", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, socketFinishConnect),
		def("reactor_open", []mma.Type{mma.TypeInt64}, mma.TypeResource, reactorOpen),
		def("reactor_ctl", []mma.Type{mma.TypeResource, mma.TypeResource, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, reactorCtl),
		def("reactor_wait", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, reactorWait),
		def("reactor_signal", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, reactorSignal),
		def("reactor_ack", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, reactorAck),
	}})
}

func def(name string, args []mma.Type, result mma.Type, function mma.NativeFunction) mma.FunctionDefinition {
	return mma.FunctionDefinition{Name: name, Public: true, Signature: mma.FunctionSignature{Parameters: args, Return: result}, Function: function}
}

func intArgument(call *mma.CallContext, index int) (int64, error) {
	arg, err := call.Argument(index)
	if err != nil {
		return 0, err
	}
	return mma.Values.AsInt64(arg)
}

func createSocket(call *mma.CallContext, state *socketState) error {
	value, err := call.CreateResource(resourceType, state, func(raw any) error {
		socket := raw.(*socketState)
		if socket.fd < 0 {
			return nil
		}
		return syscall.Close(socket.fd)
	})
	if err != nil {
		if state.fd >= 0 {
			_ = syscall.Close(state.fd)
		}
		return err
	}
	return call.Return(value)
}

func socketOpen(call *mma.CallContext) error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err == nil {
		err = configureSocket(fd)
		if err != nil {
			_ = syscall.Close(fd)
			fd = -1
		}
	}
	return createSocket(call, &socketState{fd: fd, err: err})
}

func configureSocket(fd int) error {
	if err := configureNoSigpipe(fd); err != nil {
		return err
	}
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}

func socketStatus(call *mma.CallContext) error {
	value, err := call.UseResource(0, resourceType)
	if err == nil {
		err = value.(*socketState).err
	}
	return simpleStatus(call, 1, err)
}

func sockaddr(call *mma.CallContext, addressIndex, portIndex int) (*syscall.SockaddrInet4, error) {
	arg, err := call.Argument(addressIndex)
	if err != nil {
		return nil, err
	}
	address, err := mma.Values.AsBytes(arg)
	if err != nil || len(address) != 4 {
		return nil, syscall.EINVAL
	}
	port, err := intArgument(call, portIndex)
	if err != nil || port < 0 || port > 65535 {
		return nil, syscall.EINVAL
	}
	result := &syscall.SockaddrInet4{Port: int(port)}
	copy(result.Addr[:], address)
	return result, nil
}

func socketBind(call *mma.CallContext) error {
	return bindOrConnect(call, false)
}

func socketConnect(call *mma.CallContext) error {
	return bindOrConnect(call, true)
}

func bindOrConnect(call *mma.CallContext, connect bool) error {
	address, err := sockaddr(call, 1, 2)
	if err == nil {
		var state *socketState
		state, err = useSocket(call)
		if err == nil {
			if connect {
				err = syscall.Connect(state.fd, address)
			} else {
				err = syscall.Bind(state.fd, address)
			}
		}
	}
	return simpleStatus(call, 3, err)
}

func socketListen(call *mma.CallContext) error {
	backlog, err := intArgument(call, 1)
	if err == nil && (backlog < 0 || backlog > 65535) {
		err = syscall.EINVAL
	}
	if err == nil {
		var state *socketState
		state, err = useSocket(call)
		if err == nil {
			err = syscall.Listen(state.fd, int(backlog))
		}
	}
	return simpleStatus(call, 2, err)
}

func socketAccept(call *mma.CallContext) error {
	fd := -1
	state, err := useSocket(call)
	if err == nil {
		fd, _, err = syscall.Accept(state.fd)
		if err == nil {
			err = configureSocket(fd)
			if err != nil {
				_ = syscall.Close(fd)
				fd = -1
			}
		}
	}
	return createSocket(call, &socketState{fd: fd, err: err})
}

func useSocket(call *mma.CallContext) (*socketState, error) {
	value, err := call.UseResource(0, resourceType)
	if err != nil {
		return nil, err
	}
	state := value.(*socketState)
	if state.err != nil {
		return nil, state.err
	}
	if state.fd < 0 {
		return nil, syscall.EBADF
	}
	return state, nil
}

func socketRead(call *mma.CallContext) error {
	return readOrWrite(call, true)
}

func socketWrite(call *mma.CallContext) error {
	return readOrWrite(call, false)
}

func readOrWrite(call *mma.CallContext, reading bool) error {
	state, err := useSocket(call)
	if err != nil {
		return operationStatus(call, 4, 0, false, err)
	}
	offset, err := intArgument(call, 2)
	if err != nil {
		return err
	}
	length, err := intArgument(call, 3)
	if err != nil {
		return err
	}
	if offset < 0 || length < 0 || length > maxNativeRead {
		return operationStatus(call, 4, 0, false, syscall.EINVAL)
	}
	// Validate the caller's complete byte range before touching the socket.
	// A failed destination write must never consume incoming TCP bytes.
	buffer, err := call.ReadBytes(1, offset, length)
	if err != nil {
		return operationStatus(call, 4, 0, false, syscall.EINVAL)
	}
	if length == 0 {
		return operationStatus(call, 4, 0, false, nil)
	}
	count := 0
	if reading {
		count, err = syscall.Read(state.fd, buffer)
	} else {
		count, err = socketSend(state.fd, buffer)
	}
	if reading && count > 0 {
		if writeErr := call.WriteBytes(1, offset, buffer[:count]); writeErr != nil {
			return operationStatus(call, 4, 0, false, syscall.EINVAL)
		}
	}
	return operationStatus(call, 4, int64(count), reading && count == 0 && err == nil, err)
}

// socketSetNonblocking switches O_NONBLOCK; enabled must be 0 or 1.
func socketSetNonblocking(call *mma.CallContext) error {
	enabled, err := intArgument(call, 1)
	if err == nil && (enabled < 0 || enabled > 1) {
		err = syscall.EINVAL
	}
	if err == nil {
		var state *socketState
		state, err = useSocket(call)
		if err == nil {
			err = syscall.SetNonblock(state.fd, enabled == 1)
		}
	}
	return simpleStatus(call, 2, err)
}

// socketFinishConnect completes a nonblocking connect. While the connection is still being established it reports
// "in progress" and may be called again; afterwards SO_ERROR is the result.
func socketFinishConnect(call *mma.CallContext) error {
	state, err := useSocket(call)
	if err == nil {
		var writable bool
		writable, err = socketWritable(state.fd)
		if err == nil && !writable {
			err = syscall.EINPROGRESS
		}
		if err == nil {
			var pending int
			pending, err = syscall.GetsockoptInt(state.fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
			if err == nil && pending != 0 {
				err = syscall.Errno(pending)
			}
		}
	}
	return simpleStatus(call, 1, err)
}

func socketClose(call *mma.CallContext) error {
	return simpleStatus(call, 1, call.CloseResource(0, resourceType))
}

func simpleStatus(call *mma.CallContext, index int, err error) error {
	if writeErr := call.WriteBytes(index, 0, []byte{classify(err)}); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return call.Return(mma.Values.String(err.Error()))
	}
	return call.Return(mma.Values.String(""))
}

func operationStatus(call *mma.CallContext, index int, count int64, eof bool, err error) error {
	status := make([]byte, 10)
	binary.LittleEndian.PutUint64(status[:8], uint64(count))
	if eof {
		status[8] = 1
	}
	status[9] = classify(err)
	if writeErr := call.WriteBytes(index, 0, status); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return call.Return(mma.Values.String(err.Error()))
	}
	return call.Return(mma.Values.String(""))
}

func classify(err error) byte {
	if err == nil || errors.Is(err, io.EOF) {
		return statusOK
	}
	var runtimeErr *mma.RuntimeError
	if errors.As(err, &runtimeErr) {
		switch runtimeErr.Code {
		case "closed_resource":
			return statusClosed
		case "invalid_resource", "resource_type", "invalid_argument":
			return statusInvalidArgument
		}
	}
	switch {
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.EAFNOSUPPORT):
		return statusInvalidArgument
	case errors.Is(err, syscall.EBADF), errors.Is(err, syscall.ENOTSOCK):
		return statusClosed
	case errors.Is(err, syscall.ETIMEDOUT):
		return statusTimedOut
	case errors.Is(err, syscall.ECONNREFUSED):
		return statusConnectionRefused
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return statusConnectionReset
	case errors.Is(err, syscall.EADDRINUSE):
		return statusAddressInUse
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return statusPermissionDenied
	case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK):
		return statusWouldBlock
	case errors.Is(err, syscall.EINPROGRESS):
		return statusInProgress
	default:
		return statusOther
	}
}
