package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"Mio/internal/diagnostic"
	"Mio/internal/i18n"
	"Mio/internal/lint"
	"Mio/internal/stdlib"
)

func printLintHelp(out io.Writer, language i18n.Language) {
	fmt.Fprintf(out, "%s: mio lint [--format=human|short|json] [--deny-warnings] <source.mio>\n", i18n.Text(language, i18n.KeyHelpUsage))
	fmt.Fprintln(out, "       mio lint --list-rules")
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyCmdLint))
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyLintBehavior))
	fmt.Fprintln(out, i18n.Text(language, i18n.KeyLintStatus))
}

var lintRuleDescriptions = map[string]i18n.Key{
	"unused-variable":    i18n.KeyLintUnusedVariable,
	"unused-import":      i18n.KeyLintUnusedImport,
	"unreachable-code":   i18n.KeyLintUnreachableCode,
	"constant-condition": i18n.KeyLintConstantCondition,
}

// runLint implements `mio lint`.
func runLint(args []string, stdout, stderr io.Writer, language i18n.Language) int {
	options := cliDiagnosticOptions{color: diagnostic.ColorNever, format: diagnostic.FormatHuman, language: language}
	denyWarnings := false
	path := ""
	for _, argument := range args {
		switch {
		case argument == "-h" || argument == "--help":
			printLintHelp(stdout, language)
			return 0
		case argument == "--list-rules":
			for _, name := range lint.RuleNames {
				fmt.Fprintf(stdout, "%-18s %s\n", name, i18n.Text(language, lintRuleDescriptions[name]))
			}
			return 0
		case argument == "--deny-warnings":
			denyWarnings = true
		case argument == "--format=human":
			options.format = diagnostic.FormatHuman
		case argument == "--format=short":
			options.format = diagnostic.FormatShort
		case argument == "--format=json":
			options.format = diagnostic.FormatJSON
		case strings.HasPrefix(argument, "-") || path != "" || argument == "":
			printLintHelp(stderr, language)
			return 2
		default:
			path = argument
		}
	}
	if path == "" {
		printLintHelp(stderr, language)
		return 2
	}
	registry, err := stdlib.NewRegistry()
	if err != nil {
		fmt.Fprintln(stderr, "mio:", err)
		return 2
	}
	findings, err := lint.Run(path, bunkoRoot(path), registry)
	if err != nil {
		renderCLIError(stderr, path, err, options, false)
		return 2
	}
	if options.format == diagnostic.FormatJSON {
		if findings == nil {
			findings = []lint.Finding{}
		}
		encoded, _ := json.MarshalIndent(findings, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
	} else {
		for _, f := range findings {
			fmt.Fprintf(stdout, "%s:%d:%d: %s[%s]: %s\n", f.File, f.Line, f.Column, i18n.Text(language, i18n.KeyDiagWarning), f.Rule, f.LocalizedMessage(language))
		}
	}
	if denyWarnings && len(findings) > 0 {
		return 1
	}
	return 0
}
