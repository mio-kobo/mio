//go:build !darwin

package bunkoscaffold

import "os"

func terminalInput(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
func terminalBytes(file *os.File) (func(), bool) { return nil, false }
func terminalPending(file *os.File) bool         { return false }
