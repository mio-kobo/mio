package bunkopm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxFiles = 2000
const MaxFileBytes = 8 << 20
const MaxTotalBytes = 64 << 20

var portable = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
var windowsName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)
var legalName = regexp.MustCompile(`^(LICENSE|LICENCE|COPYING|NOTICE|AUTHORS|README)(-[A-Za-z0-9.]{1,40})?$`)

func archivePath(name string) (string, error) {
	if len(name) > 200 || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", fail("BUNKO_E_ARCHIVE", "unsafe path %q", name)
	}
	n := strings.TrimSuffix(name, "/")
	parts := strings.Split(n, "/")
	if len(parts) > 12 {
		return "", fail("BUNKO_E_ARCHIVE", "path too deep")
	}
	for _, p := range parts {
		if p == "." || p == ".." || !portable.MatchString(p) || strings.HasSuffix(p, ".") || windowsName.MatchString(p) {
			return "", fail("BUNKO_E_ARCHIVE", "nonportable path %q", name)
		}
	}
	return n, nil
}
func packageFile(name string, mode int64, data []byte) error {
	base := path.Base(name)
	if !strings.HasSuffix(base, ".mio") && !strings.HasSuffix(base, ".md") && !strings.HasSuffix(base, ".txt") && !legalName.MatchString(base) {
		return fail("BUNKO_E_ARCHIVE", "file type not allowed: %q", name)
	}
	if mode&07111 != 0 {
		return fail("BUNKO_E_ARCHIVE", "executable/special mode: %q", name)
	}
	for _, magic := range []string{"\x7fELF", "MZ", "\xfe\xed\xfa\xce", "\xfe\xed\xfa\xcf", "\xce\xfa\xed\xfe", "\xcf\xfa\xed\xfe", "\xca\xfe\xba\xbe", "#!"} {
		if bytes.HasPrefix(data, []byte(magic)) {
			return fail("BUNKO_E_ARCHIVE", "executable payload: %q", name)
		}
	}
	if bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
		return fail("BUNKO_E_ARCHIVE", "not UTF-8 text: %q", name)
	}
	return nil
}

// ReadArchive validates the entire archive before callers write any files.
// A decompressed transport bound also limits hidden tar/PAX headers and padding.
func ReadArchive(data []byte) (map[string][]byte, error) {
	if len(data) > MaxArchive {
		return nil, fail("BUNKO_E_ARCHIVE", "compressed archive too large")
	}
	gz, e := gzip.NewReader(bytes.NewReader(data))
	if e != nil {
		return nil, fail("BUNKO_E_ARCHIVE", "%v", e)
	}
	defer gz.Close()
	bounded := &io.LimitedReader{R: gz, N: 80 << 20}
	tr := tar.NewReader(bounded)
	files := map[string][]byte{}
	seen := map[string]bool{}
	var total int64
	entries := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fail("BUNKO_E_ARCHIVE", "%v", e)
		}
		entries++
		if entries > 10000 {
			return nil, fail("BUNKO_E_ARCHIVE", "too many archive entries")
		}
		n, e := archivePath(h.Name)
		if e != nil {
			return nil, e
		}
		fold := strings.ToLower(n)
		if seen[fold] {
			return nil, fail("BUNKO_E_ARCHIVE", "duplicate/case collision %q", n)
		}
		seen[fold] = true
		for k := range h.PAXRecords {
			if strings.HasPrefix(k, "GNU.sparse") {
				return nil, fail("BUNKO_E_ARCHIVE", "sparse entry")
			}
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return nil, fail("BUNKO_E_ARCHIVE", "links or special entries: %q", n)
		}
		total += h.Size
		if len(files) >= MaxFiles || h.Size < 0 || h.Size > MaxFileBytes || total > MaxTotalBytes {
			return nil, fail("BUNKO_E_ARCHIVE", "archive exceeds file/size limits")
		}
		b, e := io.ReadAll(tr)
		if e != nil || int64(len(b)) != h.Size {
			return nil, fail("BUNKO_E_ARCHIVE", "truncated %q", n)
		}
		if e = packageFile(n, h.Mode, b); e != nil {
			return nil, e
		}
		files[n] = b
	}
	// Consume the gzip trailer, checking CRC and rejecting non-padding payloads.
	tail, e := io.ReadAll(bounded)
	if e != nil || bounded.N == 0 {
		return nil, fail("BUNKO_E_ARCHIVE", "corrupt or oversized gzip stream")
	}
	for _, b := range tail {
		if b != 0 {
			return nil, fail("BUNKO_E_ARCHIVE", "trailing archive payload")
		}
	}
	if len(files) == 0 {
		return nil, fail("BUNKO_E_ARCHIVE", "empty archive")
	}
	fileNames := map[string]bool{}
	for n := range files {
		fileNames[strings.ToLower(n)] = true
	}
	for n := range seen {
		p := n
		for strings.Contains(p, "/") {
			p = path.Dir(p)
			if fileNames[p] {
				return nil, fail("BUNKO_E_ARCHIVE", "file is also a directory: %q", p)
			}
		}
	}
	return files, nil
}
