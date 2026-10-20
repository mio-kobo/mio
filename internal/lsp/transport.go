package lsp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultMaxMessageSize = 64 << 20
	maxHeaderLineSize     = 8 << 10
)

var (
	errMissingContentLength = errors.New("lsp: missing Content-Length header")
	errMessageTooLarge      = errors.New("lsp: message exceeds size limit")
	errMalformedHeader      = errors.New("lsp: malformed header")
)

// transport 读写 Content-Length 帧，并串行化写入
type transport struct {
	reader         *bufio.Reader
	writer         *bufio.Writer
	writeMu        sync.Mutex
	maxMessageSize int
}

func newTransport(reader io.Reader, writer io.Writer) *transport {
	return &transport{
		reader:         bufio.NewReader(reader),
		writer:         bufio.NewWriter(writer),
		maxMessageSize: defaultMaxMessageSize,
	}
}

// read 读取一帧 JSON 负载
func (t *transport) read() ([]byte, error) {
	contentLength := -1
	headerBytes := 0
	for {
		line, err := t.reader.ReadString('\n')
		headerBytes += len(line)
		if headerBytes > maxHeaderLineSize*8 {
			return nil, fmt.Errorf("%w: header section is too large", errMalformedHeader)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("%w: %q", errMalformedHeader, line)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("%w: invalid Content-Length %q", errMalformedHeader, value)
		}
		contentLength = parsed
	}
	if contentLength < 0 {
		return nil, errMissingContentLength
	}
	if contentLength > t.maxMessageSize {
		return nil, fmt.Errorf("%w: %d bytes", errMessageTooLarge, contentLength)
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(t.reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// write 写出一帧 JSON 负载
func (t *transport) write(payload []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := fmt.Fprintf(t.writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := t.writer.Write(payload); err != nil {
		return err
	}
	return t.writer.Flush()
}
