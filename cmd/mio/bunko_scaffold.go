package main

import (
	"io"
	"os"
	"runtime"

	"Mio/internal/bunkopm"
	"Mio/internal/bunkoscaffold"
	"Mio/internal/i18n"
)

func runBunkoScaffold(args []string, in io.Reader, out, stderr io.Writer, lang i18n.Language, theme helpTheme) int {
	root, e := os.Getwd()
	if e != nil {
		return bunkoFailure(stderr, lang, e)
	}
	return bunkoscaffold.Run(args[0], args[1:], bunkoscaffold.Context{In: in, Out: out, Err: stderr, Language: lang, Root: root, Compiler: version, Host: bunkopm.Target(runtime.GOOS, runtime.GOARCH), Paint: theme.paint, Runner: bunkoscaffold.OSRunner{}})
}
