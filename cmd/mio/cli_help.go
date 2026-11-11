package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"Mio/internal/bunkoscaffold"
	"Mio/internal/i18n"
)

// The reference Go CLI renders the same catalog and semantic roles as the
// native CLI. Layout code is intentionally small; command inventory strings
// are language-neutral technical tokens.

type helpEntry struct {
	name        string
	group       int
	description i18n.Key
	usage       string
	options     []string
}

type helpOption struct {
	name        string
	syntax      string
	description i18n.Key
}

const (
	helpGroupGeneral = iota
	helpGroupCompilation
	helpGroupExecution
	helpGroupDiagnostics
	helpGroupTesting
	helpGroupTools
)

func helpCommands() []helpEntry {
	return []helpEntry{
		{name: "bunko", group: helpGroupTools, description: i18n.KeyCmdBunko, usage: "mio bunko <init|add|remove|install|list|info|search|create|connect> [arguments]", options: []string{"lang", "color", "theme"}},
		{name: "help", group: helpGroupGeneral, description: i18n.KeyCmdHelp, usage: "mio help [command]", options: []string{"lang", "color", "theme"}},
		{name: "version", group: helpGroupGeneral, description: i18n.KeyCmdVersion, usage: "mio version", options: []string{"lang", "color", "theme"}},
		{name: "build", group: helpGroupCompilation, description: i18n.KeyCmdBuild, usage: "mio build <source> [options]", options: []string{"output", "target", "emit", "symbols", "gc_stress", "observe", "bunko", "format", "color", "theme", "lang", "verbose"}},
		{name: "check", group: helpGroupCompilation, description: i18n.KeyCmdCheck, usage: "mio check <source> [options]", options: []string{"bunko", "format", "color", "theme", "lang", "verbose"}},
		{name: "run", group: helpGroupExecution, description: i18n.KeyCmdRun, usage: "mio run <source> [-- <program arguments...>]", options: []string{"target", "bunko", "format", "color", "theme", "lang", "verbose", "program_args"}},
		{name: "exec", group: helpGroupExecution, description: i18n.KeyCmdExec, usage: "mio exec <file.mochi> [-- <program arguments...>]", options: []string{"target", "format", "color", "theme", "lang", "verbose", "program_args"}},
		{name: "disasm", group: helpGroupDiagnostics, description: i18n.KeyCmdDisasm, usage: "mio disasm <file.mochi>", options: []string{"lang", "color", "theme"}},
		{name: "test", group: helpGroupTesting, description: i18n.KeyCmdTest, usage: "mio test <directory[/...]> [--run=<regexp>] [options]", options: []string{"run_filter", "format", "color", "theme", "lang", "verbose", "bunko"}},
		{name: "fmt", group: helpGroupTools, description: i18n.KeyCmdFmt, usage: "mio fmt [--check] <path>..."},
		{name: "lint", group: helpGroupTools, description: i18n.KeyCmdLint, usage: "mio lint [options] <source.mio>"},
		{name: "lsp", group: helpGroupTools, description: i18n.KeyCmdLsp, usage: "mio lsp [--stdio] [options]", options: []string{"stdio", "bunko"}},
	}
}

func helpOptions() []helpOption {
	return []helpOption{
		{name: "lang", syntax: "--lang <locale>", description: i18n.KeyOptLang},
		{name: "color", syntax: "--color <when>", description: i18n.KeyOptColor},
		{name: "theme", syntax: "--theme <name>", description: i18n.KeyOptTheme},
		{name: "format", syntax: "--format <format>", description: i18n.KeyOptFormat},
		{name: "verbose", syntax: "--verbose", description: i18n.KeyOptVerbose},
		{name: "bunko", syntax: "--bunko-root <directory>", description: i18n.KeyOptBunko},
		{name: "output", syntax: "-o <path>", description: i18n.KeyOptOutput},
		{name: "target", syntax: "--target <target>", description: i18n.KeyOptTarget},
		{name: "emit", syntax: "--emit=mochi", description: i18n.KeyOptEmit},
		{name: "symbols", syntax: "--emit-symbols <file>", description: i18n.KeyOptSymbols},
		{name: "gc_stress", syntax: "--gc-stress", description: i18n.KeyOptGcStress},
		{name: "observe", syntax: "--observe", description: i18n.KeyOptObserve},
		{name: "run_filter", syntax: "--run=<regexp>", description: i18n.KeyOptRunFilter},
		{name: "stdio", syntax: "--stdio", description: i18n.KeyOptStdio},
		{name: "program_args", syntax: "-- <arguments...>", description: i18n.KeyOptProgramArgs},
		{name: "help", syntax: "--help, -h", description: i18n.KeyOptHelp},
		{name: "version", syntax: "--version", description: i18n.KeyOptVersion},
	}
}

func helpGroupKey(group int) i18n.Key {
	switch group {
	case helpGroupCompilation:
		return i18n.KeyHelpGroupCompilation
	case helpGroupExecution:
		return i18n.KeyHelpGroupExecution
	case helpGroupDiagnostics:
		return i18n.KeyHelpGroupDiagnostics
	case helpGroupTesting:
		return i18n.KeyHelpGroupTesting
	case helpGroupTools:
		return i18n.KeyHelpGroupTools
	default:
		return i18n.KeyHelpGroupGeneral
	}
}

func findHelpCommand(name string) (helpEntry, bool) {
	for _, entry := range helpCommands() {
		if entry.name == name {
			return entry, true
		}
	}
	return helpEntry{}, false
}

func findHelpOption(name string) (helpOption, bool) {
	for _, option := range helpOptions() {
		if option.name == name {
			return option, true
		}
	}
	return helpOption{}, false
}

type helpTheme struct {
	enabled bool
	depth   int
	palette int
}

const (
	helpPaletteNeutral = iota
	helpPaletteDark
	helpPaletteLight
	helpDepthANSI = 1
	helpDepth256  = 2
	helpDepthTrue = 3
)

func helpColorEnabled(mode, noColor, term, clicolor, force, ci string, tty bool) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if noColor != "" || term == "dumb" || clicolor == "0" {
		return false
	}
	if force != "" && force != "0" {
		return true
	}
	if ci != "" {
		return false
	}
	return tty
}

func helpPalette(selected, configured, colorfgbg string) int {
	switch selected {
	case "dark":
		return helpPaletteDark
	case "light":
		return helpPaletteLight
	}
	switch configured {
	case "dark":
		return helpPaletteDark
	case "light":
		return helpPaletteLight
	}
	background := -1
	for _, field := range strings.Split(colorfgbg, ";") {
		if value, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			background = value
		}
	}
	if background >= 0 && background <= 15 {
		if background <= 6 || background == 8 {
			return helpPaletteDark
		}
		return helpPaletteLight
	}
	return helpPaletteNeutral
}

func helpColorDepth(colorterm, term string) int {
	if colorterm == "truecolor" || colorterm == "24bit" {
		return helpDepthTrue
	}
	if strings.Contains(term, "256color") {
		return helpDepth256
	}
	return helpDepthANSI
}

func resolveHelpTheme(colorMode, themeName string, out io.Writer) helpTheme {
	tty := false
	if file, ok := out.(*os.File); ok {
		if info, err := file.Stat(); err == nil {
			tty = info.Mode()&os.ModeCharDevice != 0
		}
	}
	enabled := helpColorEnabled(colorMode, os.Getenv("NO_COLOR"), os.Getenv("TERM"), os.Getenv("CLICOLOR"), os.Getenv("CLICOLOR_FORCE"), os.Getenv("CI"), tty)
	palette := helpPalette(themeName, os.Getenv("MIO_THEME"), os.Getenv("COLORFGBG"))
	if !enabled {
		return helpTheme{enabled: false, depth: helpDepthANSI, palette: palette}
	}
	depth := helpColorDepth(os.Getenv("COLORTERM"), os.Getenv("TERM"))
	if palette == helpPaletteNeutral {
		depth = helpDepthANSI
	}
	return helpTheme{enabled: true, depth: depth, palette: palette}
}

func (theme helpTheme) code(role string) string {
	dark := theme.palette == helpPaletteDark
	if role == "title" {
		if theme.depth == helpDepthTrue {
			if dark {
				return "\x1b[1;38;2;124;196;255m"
			}
			return "\x1b[1;38;2;11;79;138m"
		}
		if theme.depth == helpDepth256 {
			if dark {
				return "\x1b[1;38;5;81m"
			}
			return "\x1b[1;38;5;25m"
		}
		return "\x1b[1m"
	}
	codes := map[string]string{
		"command": "\x1b[36m",
		"option":  "\x1b[33m",
		"success": "\x1b[32m",
		"warning": "\x1b[33m",
		"error":   "\x1b[31m",
		"muted":   "\x1b[90m",
	}
	codes256 := map[string][2]string{
		"command": {"\x1b[38;5;45m", "\x1b[38;5;30m"},
		"option":  {"\x1b[38;5;220m", "\x1b[38;5;136m"},
		"success": {"\x1b[38;5;114m", "\x1b[38;5;28m"},
		"warning": {"\x1b[38;5;214m", "\x1b[38;5;166m"},
		"error":   {"\x1b[38;5;203m", "\x1b[38;5;160m"},
		"muted":   {"\x1b[38;5;244m", "\x1b[38;5;240m"},
	}
	trueColors := map[string][2]string{
		"command": {"\x1b[38;2;86;212;221m", "\x1b[38;2;0;109;119m"},
		"option":  {"\x1b[38;2;255;216;102m", "\x1b[38;2;122;91;0m"},
		"success": {"\x1b[38;2;126;231;135m", "\x1b[38;2;23;107;44m"},
		"warning": {"\x1b[38;2;255;166;87m", "\x1b[38;2;154;74;0m"},
		"error":   {"\x1b[38;2;255;123;114m", "\x1b[38;2;164;0;36m"},
		"muted":   {"\x1b[38;2;139;148;158m", "\x1b[38;2;92;102;112m"},
	}
	set, ok := codes[role]
	if !ok {
		return ""
	}
	if theme.depth == helpDepthTrue {
		pair := trueColors[role]
		if dark {
			return pair[0]
		}
		return pair[1]
	}
	if theme.depth == helpDepth256 {
		pair := codes256[role]
		if dark {
			return pair[0]
		}
		return pair[1]
	}
	return set
}

func (theme helpTheme) paint(role, value string) string {
	if !theme.enabled || value == "" {
		return value
	}
	code := theme.code(role)
	if code == "" {
		return value
	}
	return code + value + "\x1b[0m"
}

func helpCell(value rune) int {
	if value < 32 || unicode.Is(unicode.Mn, value) || value == 0x200D {
		return 0
	}
	if (value >= 0x1100 && value <= 0x115F) || (value >= 0x2E80 && value <= 0xA4CF) ||
		(value >= 0xAC00 && value <= 0xD7A3) || (value >= 0xF900 && value <= 0xFAFF) ||
		(value >= 0xFE30 && value <= 0xFE4F) || (value >= 0xFF00 && value <= 0xFF60) ||
		(value >= 0xFFE0 && value <= 0xFFE6) || (value >= 0x1F300 && value <= 0x1F64F) ||
		(value >= 0x1F900 && value <= 0x1F9FF) || (value >= 0x20000 && value <= 0x3FFFD) {
		return 2
	}
	return 1
}

func helpWidth(value string) int {
	width := 0
	for _, character := range value {
		width += helpCell(character)
	}
	return width
}

func helpWrap(value string, limit int) []string {
	if limit < 4 {
		limit = 4
	}
	var lines []string
	var current strings.Builder
	width := 0
	for _, word := range strings.Fields(value) {
		size := helpWidth(word)
		if width == 0 {
			if size <= limit {
				current.WriteString(word)
				width = size
				continue
			}
			var chunk strings.Builder
			chunkWidth := 0
			for _, character := range word {
				cell := helpCell(character)
				if chunkWidth+cell > limit && chunkWidth > 0 {
					lines = append(lines, chunk.String())
					chunk.Reset()
					chunkWidth = 0
				}
				chunk.WriteRune(character)
				chunkWidth += cell
			}
			current.WriteString(chunk.String())
			width = chunkWidth
			continue
		}
		if width+1+size <= limit {
			current.WriteString(" ")
			current.WriteString(word)
			width += 1 + size
			continue
		}
		lines = append(lines, current.String())
		current.Reset()
		width = 0
		if size <= limit {
			current.WriteString(word)
			width = size
			continue
		}
		var chunk strings.Builder
		chunkWidth := 0
		for _, character := range word {
			cell := helpCell(character)
			if chunkWidth+cell > limit && chunkWidth > 0 {
				lines = append(lines, chunk.String())
				chunk.Reset()
				chunkWidth = 0
			}
			chunk.WriteRune(character)
			chunkWidth += cell
		}
		current.WriteString(chunk.String())
		width = chunkWidth
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}

func helpSpaces(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(" ", count)
}

func helpColumn(syntaxes []string, width int) int {
	maximum := 0
	for _, syntax := range syntaxes {
		if size := helpWidth(syntax) + 4; size > maximum {
			maximum = size
		}
	}
	if maximum > width-20 {
		maximum = width - 20
	}
	if maximum > 34 {
		maximum = 34
	}
	if maximum < 12 {
		maximum = 12
	}
	return maximum
}

func helpWidthFromEnv() int {
	if columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && columns >= 20 && columns <= 1000 {
		return columns
	}
	return 80
}

func writeHelpText(out io.Writer, text string) {
	fmt.Fprint(out, text)
}

func writeHelpEntry(out io.Writer, theme helpTheme, syntax, description string, width, column int, role string) {
	indent := 2
	syntaxWidth := helpWidth(syntax)
	if width >= 44 && column > syntaxWidth+4 {
		lines := helpWrap(description, width-column)
		fmt.Fprintf(out, "%s%s%s\n", helpSpaces(indent), theme.paint(role, syntax), helpSpaces(column-indent-syntaxWidth)+lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(out, "%s%s\n", helpSpaces(column), line)
		}
		return
	}
	fmt.Fprintf(out, "%s%s\n", helpSpaces(indent), theme.paint(role, syntax))
	for _, line := range helpWrap(description, width-indent-2) {
		fmt.Fprintf(out, "%s%s\n", helpSpaces(indent+2), line)
	}
}

func printUsage(out io.Writer, language i18n.Language, theme helpTheme) {
	width := helpWidthFromEnv()
	fmt.Fprintln(out, theme.paint("title", i18n.Format(language, i18n.KeyHelpTitle, version)))
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyHelpTagline))
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpUsage)))
	fmt.Fprintln(out, "  mio <command> [options]")
	fmt.Fprintln(out, "  mio help <command>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpCommands)))
	commands := helpCommands()
	syntaxes := make([]string, 0, len(commands))
	for _, entry := range commands {
		syntaxes = append(syntaxes, strings.TrimPrefix(entry.usage, "mio "))
	}
	column := helpColumn(syntaxes, width)
	for group := helpGroupGeneral; group <= helpGroupTools; group++ {
		first := true
		for index, entry := range commands {
			if entry.group != group {
				continue
			}
			if first {
				fmt.Fprintf(out, "  %s\n", theme.paint("muted", i18n.Text(language, helpGroupKey(group))))
				first = false
			}
			writeHelpEntry(out, theme, syntaxes[index], i18n.Text(language, entry.description), width, column, "command")
		}
		if !first {
			fmt.Fprintln(out)
		}
	}
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpOptions)))
	globalOptions := []string{"lang", "color", "theme", "format", "verbose", "bunko", "help", "version"}
	globalSyntaxes := make([]string, 0, len(globalOptions))
	for _, name := range globalOptions {
		if option, ok := findHelpOption(name); ok {
			globalSyntaxes = append(globalSyntaxes, option.syntax)
		}
	}
	optionColumn := helpColumn(globalSyntaxes, width)
	for _, name := range globalOptions {
		if option, ok := findHelpOption(name); ok {
			writeHelpEntry(out, theme, option.syntax, i18n.Text(language, option.description), width, optionColumn, "option")
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpExamples)))
	fmt.Fprintln(out, "  mio help build")
	fmt.Fprintln(out, "  mio --lang ja-JP help")
	fmt.Fprintln(out, "  mio test . --lang=zh-CN")
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("muted", i18n.Text(language, i18n.KeyHelpMore)))
}

func printCommandHelp(out io.Writer, language i18n.Language, theme helpTheme, name string) bool {
	switch name {
	case "fmt":
		printFmtHelp(out, language)
		return true
	case "lint":
		printLintHelp(out, language)
		return true
	}
	entry, ok := findHelpCommand(name)
	if !ok {
		return false
	}
	width := helpWidthFromEnv()
	fmt.Fprintln(out, theme.paint("command", "mio "+name))
	fmt.Fprintln(out, i18n.Text(language, entry.description))
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpUsage)))
	fmt.Fprintln(out, "  "+entry.usage)
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpOptions)))
	syntaxes := make([]string, 0, len(entry.options))
	for _, optionName := range entry.options {
		if option, ok := findHelpOption(optionName); ok {
			syntaxes = append(syntaxes, option.syntax)
		}
	}
	column := helpColumn(syntaxes, width)
	for _, optionName := range entry.options {
		if option, ok := findHelpOption(optionName); ok {
			writeHelpEntry(out, theme, option.syntax, i18n.Text(language, option.description), width, column, "option")
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, theme.paint("title", i18n.Text(language, i18n.KeyHelpExamples)))
	for _, example := range helpExamples(name) {
		fmt.Fprintln(out, "  "+example)
	}
	return true
}

func helpExamples(name string) []string {
	switch name {
	case "build":
		return []string{"mio build main.mio", "mio build main.mio -o app"}
	case "check":
		return []string{"mio check main.mio", "mio check main.mio --format=short"}
	case "run":
		return []string{"mio run main.mio"}
	case "exec":
		return []string{"mio exec app.mochi"}
	case "disasm":
		return []string{"mio disasm app.mochi"}
	case "test":
		return []string{"mio test .", "mio test . --run=string --format=short"}
	case "lsp":
		return []string{"mio lsp --stdio"}
	case "version":
		return []string{"mio version", "mio version --lang=ja-JP"}
	default:
		return []string{"mio help", "mio help build"}
	}
}

type parsedGlobals struct {
	args     []string
	language string
	theme    string
	color    string
	errKey   i18n.Key
	detail   string
}

func globalOptionValue(argument, prefix string) string {
	if strings.HasPrefix(argument, prefix) {
		return argument[len(prefix):]
	}
	return ""
}

func globalOptionTakesValue(argument string) bool {
	if argument != "--version" && bunkoscaffold.TakesValue(argument) {
		return true
	}
	switch argument {
	case "-o", "--bunko-root", "--target", "--emit-symbols":
		return true
	}
	return false
}

// parseGlobalOptions removes --lang/--theme/--color from anywhere before -- so
// that "mio --lang ja-JP help", "mio help --lang ja-JP" and
// "mio test . --lang=de-DE" all behave identically. Values of ordinary
// options and everything after -- are preserved verbatim.
func parseGlobalOptions(args []string) parsedGlobals {
	result := parsedGlobals{}
	for at := 0; at < len(args) && result.errKey == ""; {
		argument := args[at]
		if argument == "--" {
			result.args = append(result.args, args[at:]...)
			at = len(args)
			break
		}
		switch {
		case argument == "--lang" || strings.HasPrefix(argument, "--lang="):
			value := globalOptionValue(argument, "--lang=")
			if argument == "--lang" {
				if at+1 >= len(args) || strings.HasPrefix(args[at+1], "-") {
					result.errKey, result.detail = i18n.KeyErrMissingValue, "--lang"
					at++
					continue
				}
				at++
				value = args[at]
			}
			if value == "" {
				result.errKey, result.detail = i18n.KeyErrMissingValue, "--lang"
			} else {
				result.language = value
			}
			at++
		case argument == "--theme" || strings.HasPrefix(argument, "--theme="):
			value := globalOptionValue(argument, "--theme=")
			if argument == "--theme" {
				if at+1 >= len(args) || strings.HasPrefix(args[at+1], "-") {
					result.errKey, result.detail = i18n.KeyErrMissingValue, "--theme"
					at++
					continue
				}
				at++
				value = args[at]
			}
			if value != "auto" && value != "dark" && value != "light" {
				result.errKey, result.detail = i18n.KeyErrInvalidTheme, value
			} else {
				result.theme = value
			}
			at++
		case argument == "--color" || strings.HasPrefix(argument, "--color="):
			value := globalOptionValue(argument, "--color=")
			if argument == "--color" {
				if at+1 >= len(args) || strings.HasPrefix(args[at+1], "-") {
					result.errKey, result.detail = i18n.KeyErrMissingValue, "--color"
					at++
					continue
				}
				at++
				value = args[at]
			}
			if value != "auto" && value != "always" && value != "never" {
				result.errKey, result.detail = i18n.KeyErrInvalidColor, value
			} else {
				result.color = value
			}
			at++
		case (globalOptionTakesValue(argument) || (argument == "--version" && len(result.args) >= 2 && result.args[0] == "bunko" && result.args[1] == "create")) && at+1 < len(args):
			result.args = append(result.args, argument, args[at+1])
			at += 2
		default:
			result.args = append(result.args, argument)
			at++
		}
	}
	return result
}
