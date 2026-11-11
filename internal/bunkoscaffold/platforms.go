package bunkoscaffold

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (c *Context) platformLabel(target string) string {
	key := "platform_" + strings.ReplaceAll(target, "/", "_")
	label := c.text(key)
	if target == c.Host {
		label += "  " + c.text("current_device")
	}
	return label
}
func (c *Context) choosePlatforms() ([]string, error) {
	targets := Targets()
	c.print("select_platforms")
	if in, ok := c.In.(*os.File); ok {
		if out, ok := c.Out.(*os.File); ok && terminalInput(out) && os.Getenv("TERM") != "dumb" {
			if restore, ok := terminalBytes(in); ok {
				defer restore()
				c.print("keys")
				selected := make([]bool, len(targets))
				cursor := 0
				for {
					for i, t := range targets {
						mark, pointer := "[ ]", " "
						if selected[i] {
							mark = "[x]"
						}
						if i == cursor {
							pointer = "›"
						}
						label := fmt.Sprintf("%s %s %s", pointer, mark, c.platformLabel(t))
						if i == cursor && c.Paint != nil {
							label = c.Paint("command", label)
						}
						fmt.Fprintln(c.Out, label)
					}
					b, err := c.reader.ReadByte()
					if err != nil {
						return nil, invalid("stdin", "")
					}
					switch b {
					case 3:
						return nil, invalid("cancelled", "")
					case 27:
						if c.reader.Buffered() == 0 && !terminalPending(in) {
							return nil, invalid("cancelled", "")
						}
						a, _ := c.reader.ReadByte()
						if a == '[' {
							b, _ = c.reader.ReadByte()
							if b == 'A' {
								cursor = (cursor + len(targets) - 1) % len(targets)
							}
							if b == 'B' {
								cursor = (cursor + 1) % len(targets)
							}
						}
					case ' ':
						selected[cursor] = !selected[cursor]
					case '\r', '\n':
						var result []string
						for i, t := range targets {
							if selected[i] {
								result = append(result, t)
							}
						}
						if len(result) > 0 {
							return result, nil
						}
					}
					// Cursor movement is a terminal protocol, not a fixed color code.
					fmt.Fprintf(c.Out, "\x1b[%dA", len(targets))
				}
			}
		}
	}
	for i, t := range targets {
		fmt.Fprintf(c.Out, "  %d. [ ] %s\n", i+1, c.platformLabel(t))
	}
	for {
		v, err := c.line("numbers")
		if err != nil {
			return nil, err
		}
		if v == "\x1b" || v == "\x03" {
			return nil, invalid("cancelled", "")
		}
		var chosen []string
		valid := true
		seen := map[int]bool{}
		for _, value := range split(v) {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > len(targets) || seen[n] {
				valid = false
				break
			}
			seen[n] = true
			chosen = append(chosen, targets[n-1])
		}
		if valid && len(chosen) > 0 {
			return chosen, nil
		}
		c.failure(invalid("invalid_platform", v))
	}
}
