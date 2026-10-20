package lsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// pathFromURI 将 file: URI 转换为本地路径；其他 scheme 返回 false。
func pathFromURI(uri string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(uri), "file:") {
		return "", false
	}
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", false
	}
	path := parsed.Path
	if path == "" {
		return "", false
	}
	if runtime.GOOS == "windows" {
		if len(path) >= 3 && path[0] == '/' && isWindowsDrive(path[1], path[2]) {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
	}
	return filepath.Clean(path), true
}

// uriFromPath 将本地路径转换为 file: URI。
func uriFromPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	slashed := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	parsed := url.URL{Scheme: "file", Path: slashed}
	return parsed.String()
}

func isWindowsDrive(letter, colon byte) bool {
	if colon != ':' {
		return false
	}
	return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
}
