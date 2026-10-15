// Package nativefs 提供 kiso/fs 的宿主文件系统适配。
package nativefs

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"

	"Mio/mma"
)

const (
	modulePath        = "mio/fs/runtime"
	fileResource      = "kiso/fs.File"
	directoryResource = "kiso/fs.DirectoryIterator"
	statusSize        = 10
	maxNativeIO       = 256 << 20
)

const (
	statusOK byte = iota
	statusNotFound
	statusPermission
	statusClosed
	statusInvalidArgument
	statusIO
	statusNoSpace
	statusInvalidPath
)

type fileState struct {
	file *os.File
	err  error
}

type directoryState struct {
	file    *os.File
	current os.DirEntry
	err     error
}

// Register 注册文件系统原生模块
func Register(registry *mma.ModuleRegistry) error {
	return registry.RegisterModule(mma.ModuleDefinition{Path: modulePath, Functions: []mma.FunctionDefinition{
		definition("open", []mma.Type{mma.TypeString, mma.TypeInt64}, mma.TypeResource, openFile),
		definition("file_status", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, fileStatus),
		definition("read", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, readFile),
		definition("write", []mma.Type{mma.TypeResource, mma.TypeBytes, mma.TypeInt64, mma.TypeInt64, mma.TypeBytes}, mma.TypeString, writeFile),
		definition("close", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, closeFile),
		definition("stat", []mma.Type{mma.TypeString, mma.TypeBytes}, mma.TypeString, statPath),
		definition("mkdir", []mma.Type{mma.TypeString, mma.TypeBytes}, mma.TypeString, makeDirectory),
		definition("open_dir", []mma.Type{mma.TypeString}, mma.TypeResource, openDirectory),
		definition("dir_status", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, directoryStatus),
		definition("dir_next", []mma.Type{mma.TypeResource}, mma.TypeBool, directoryNext),
		definition("dir_name", []mma.Type{mma.TypeResource}, mma.TypeString, directoryName),
		definition("dir_is_dir", []mma.Type{mma.TypeResource}, mma.TypeBool, directoryIsDir),
		definition("dir_is_file", []mma.Type{mma.TypeResource}, mma.TypeBool, directoryIsFile),
		definition("dir_close", []mma.Type{mma.TypeResource, mma.TypeBytes}, mma.TypeString, closeDirectory),
	}})
}

func definition(name string, parameters []mma.Type, result mma.Type, function mma.NativeFunction) mma.FunctionDefinition {
	return mma.FunctionDefinition{Name: name, Public: true, Signature: mma.FunctionSignature{Parameters: parameters, Return: result}, Function: function}
}

func argumentString(call *mma.CallContext, index int) (string, error) {
	value, err := call.Argument(index)
	if err != nil {
		return "", err
	}
	return mma.Values.AsString(value)
}

func argumentInt(call *mma.CallContext, index int) (int64, error) {
	value, err := call.Argument(index)
	if err != nil {
		return 0, err
	}
	return mma.Values.AsInt64(value)
}

func returnString(call *mma.CallContext, value string) error {
	return call.Return(mma.Values.String(value))
}

func openFile(call *mma.CallContext) error {
	path, err := argumentString(call, 0)
	if err != nil {
		return err
	}
	mode, err := argumentInt(call, 1)
	if err != nil {
		return err
	}
	var flags int
	switch mode {
	case 0:
		flags = os.O_RDONLY
	case 1:
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	case 2:
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	default:
		err = errors.New("invalid file open mode")
	}
	state := &fileState{err: err}
	if err == nil {
		if strings.IndexByte(path, 0) >= 0 {
			state.err = syscall.EINVAL
		} else {
			state.file, state.err = os.OpenFile(path, flags, 0o666)
		}
	}
	resource, err := call.CreateResource(fileResource, state, func(value any) error {
		file := value.(*fileState)
		if file.file == nil {
			return nil
		}
		return file.file.Close()
	})
	if err != nil {
		if state.file != nil {
			_ = state.file.Close()
		}
		return err
	}
	return call.Return(resource)
}

func fileStatus(call *mma.CallContext) error {
	value, err := call.UseResource(0, fileResource)
	if err != nil {
		return err
	}
	state := value.(*fileState)
	if err := writeSimpleStatus(call, 1, classify(state.err)); err != nil {
		return err
	}
	return returnString(call, errorMessage(state.err))
}

func readFile(call *mma.CallContext) error {
	value, err := call.UseResource(0, fileResource)
	if err != nil {
		return writeOperationFailure(call, 4, err)
	}
	state := value.(*fileState)
	if state.err != nil || state.file == nil {
		return writeOperationFailure(call, 4, state.err)
	}
	offset, err := argumentInt(call, 2)
	if err != nil {
		return err
	}
	length, err := argumentInt(call, 3)
	if err != nil {
		return err
	}
	if offset < 0 || length < 0 || length > maxNativeIO {
		return writeOperationStatus(call, 4, 0, false, statusInvalidArgument, "invalid byte range")
	}
	buffer := make([]byte, int(length))
	count, readErr := state.file.Read(buffer)
	if count != 0 {
		if err := call.WriteBytes(1, offset, buffer[:count]); err != nil {
			return writeOperationStatus(call, 4, 0, false, statusInvalidArgument, err.Error())
		}
	}
	eof := errors.Is(readErr, io.EOF)
	if eof {
		readErr = nil
	}
	return writeOperationStatus(call, 4, int64(count), eof, classify(readErr), errorMessage(readErr))
}

func writeFile(call *mma.CallContext) error {
	value, err := call.UseResource(0, fileResource)
	if err != nil {
		return writeOperationFailure(call, 4, err)
	}
	state := value.(*fileState)
	if state.err != nil || state.file == nil {
		return writeOperationFailure(call, 4, state.err)
	}
	offset, err := argumentInt(call, 2)
	if err != nil {
		return err
	}
	length, err := argumentInt(call, 3)
	if err != nil {
		return err
	}
	if offset < 0 || length < 0 || length > maxNativeIO {
		return writeOperationStatus(call, 4, 0, false, statusInvalidArgument, "invalid byte range")
	}
	data, err := call.ReadBytes(1, offset, length)
	if err != nil {
		return writeOperationStatus(call, 4, 0, false, statusInvalidArgument, err.Error())
	}
	count, writeErr := state.file.Write(data)
	if writeErr == nil && count != len(data) {
		writeErr = io.ErrShortWrite
	}
	return writeOperationStatus(call, 4, int64(count), false, classify(writeErr), errorMessage(writeErr))
}

func closeFile(call *mma.CallContext) error {
	err := call.CloseResource(0, fileResource)
	if writeErr := writeSimpleStatus(call, 1, classify(err)); writeErr != nil {
		return writeErr
	}
	return returnString(call, errorMessage(err))
}

func statPath(call *mma.CallContext) error {
	path, err := argumentString(call, 0)
	if err != nil {
		return err
	}
	var info os.FileInfo
	if strings.IndexByte(path, 0) >= 0 {
		err = syscall.EINVAL
	} else {
		info, err = os.Stat(path)
	}
	status := []byte{0, classify(err)}
	if err == nil {
		status[0] = 1
		if info.IsDir() {
			status[0] = 2
		}
	}
	if err := call.WriteBytes(1, 0, status); err != nil {
		return err
	}
	return returnString(call, errorMessage(err))
}

func makeDirectory(call *mma.CallContext) error {
	path, err := argumentString(call, 0)
	if err != nil {
		return err
	}
	if strings.IndexByte(path, 0) >= 0 {
		err = syscall.EINVAL
	} else {
		err = os.Mkdir(path, 0o777)
	}
	if writeErr := writeSimpleStatus(call, 1, classify(err)); writeErr != nil {
		return writeErr
	}
	return returnString(call, errorMessage(err))
}

func openDirectory(call *mma.CallContext) error {
	path, err := argumentString(call, 0)
	if err != nil {
		return err
	}
	state := &directoryState{}
	if strings.IndexByte(path, 0) >= 0 {
		state.err = syscall.EINVAL
	} else {
		state.file, state.err = os.Open(path)
	}
	resource, err := call.CreateResource(directoryResource, state, func(value any) error {
		directory := value.(*directoryState)
		if directory.file == nil {
			return nil
		}
		return directory.file.Close()
	})
	if err != nil {
		return err
	}
	return call.Return(resource)
}

func directoryStatus(call *mma.CallContext) error {
	value, err := call.UseResource(0, directoryResource)
	if err != nil {
		return err
	}
	state := value.(*directoryState)
	if err := writeSimpleStatus(call, 1, classify(state.err)); err != nil {
		return err
	}
	return returnString(call, errorMessage(state.err))
}

func directoryNext(call *mma.CallContext) error {
	value, err := call.UseResource(0, directoryResource)
	if err != nil {
		return err
	}
	state := value.(*directoryState)
	if state.err != nil || state.file == nil {
		state.current = nil
		return call.Return(mma.Values.Bool(false))
	}
	entries, readErr := state.file.ReadDir(1)
	if errors.Is(readErr, io.EOF) {
		state.current = nil
		return call.Return(mma.Values.Bool(false))
	}
	if readErr != nil {
		state.err = readErr
		state.current = nil
		return call.Return(mma.Values.Bool(false))
	}
	state.current = entries[0]
	return call.Return(mma.Values.Bool(true))
}

func directoryName(call *mma.CallContext) error {
	value, err := call.UseResource(0, directoryResource)
	if err != nil {
		return err
	}
	state := value.(*directoryState)
	if state.current == nil {
		return mma.Errors.New("invalid_argument", "目录迭代器没有当前项", nil)
	}
	return returnString(call, state.current.Name())
}

func directoryIsDir(call *mma.CallContext) error {
	value, err := call.UseResource(0, directoryResource)
	if err != nil {
		return err
	}
	state := value.(*directoryState)
	if state.current == nil {
		return mma.Errors.New("invalid_argument", "目录迭代器没有当前项", nil)
	}
	return call.Return(mma.Values.Bool(state.current.IsDir()))
}

func directoryIsFile(call *mma.CallContext) error {
	value, err := call.UseResource(0, directoryResource)
	if err != nil {
		return err
	}
	state := value.(*directoryState)
	if state.current == nil {
		return mma.Errors.New("invalid_argument", "目录迭代器没有当前项", nil)
	}
	return call.Return(mma.Values.Bool(state.current.Type().IsRegular()))
}

func closeDirectory(call *mma.CallContext) error {
	err := call.CloseResource(0, directoryResource)
	if writeErr := writeSimpleStatus(call, 1, classify(err)); writeErr != nil {
		return writeErr
	}
	return returnString(call, errorMessage(err))
}

func writeSimpleStatus(call *mma.CallContext, index int, code byte) error {
	return call.WriteBytes(index, 0, []byte{code})
}

func writeOperationFailure(call *mma.CallContext, statusIndex int, err error) error {
	return writeOperationStatus(call, statusIndex, 0, false, classify(err), errorMessage(err))
}

func writeOperationStatus(call *mma.CallContext, statusIndex int, count int64, eof bool, code byte, message string) error {
	status := make([]byte, statusSize)
	binary.LittleEndian.PutUint64(status[:8], uint64(count))
	if eof {
		status[8] = 1
	}
	status[9] = code
	if err := call.WriteBytes(statusIndex, 0, status); err != nil {
		return err
	}
	return returnString(call, message)
}

func classify(err error) byte {
	if err == nil {
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
	case errors.Is(err, os.ErrNotExist):
		return statusNotFound
	case errors.Is(err, os.ErrPermission):
		return statusPermission
	case errors.Is(err, os.ErrClosed):
		return statusClosed
	case errors.Is(err, syscall.ENOSPC):
		return statusNoSpace
	case errors.Is(err, syscall.EINVAL):
		return statusInvalidPath
	default:
		return statusIO
	}
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
