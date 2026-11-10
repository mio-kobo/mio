package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"Mio/internal/diagnostic"
	"Mio/internal/lint"
	"Mio/internal/stdlib"
)

const lintUsage = `Usage: mio lint [--format=human|short|json] [--deny-warnings] <source.mio>
       mio lint --list-rules

Run optional static-analysis rules on a Mio source file. The file must first
pass the same checks as "mio check"; compiler errors are reported as errors
(exit status 2) and no lint rules run. Lint findings are warnings.

Options:
  --format=human|short   file:line:column: warning[rule]: message (default)
  --format=json          JSON array of findings
  --deny-warnings        Exit with status 1 when any warning is reported.
  --list-rules           Print the available rules.
  -h, --help             Show this help.

Exit status: 0 no findings (or warnings without --deny-warnings), 1 warnings
with --deny-warnings, 2 usage error or compiler error.

Rules: unused-variable, unused-import, unreachable-code, constant-condition.
Rules are conservative and never report a pattern that is not certain.
`

var lintRuleDescriptions = map[string]string{
	"unused-variable":    "function-local variable that is never referenced (names starting with _ are exempt)",
	"unused-import":      "yobu import whose package name never appears in the file",
	"unreachable-code":   "statement after kaesu, nukeru or tsuzuku in the same block",
	"constant-condition": "moshi condition that is literally hai/iie, or aida (iie)",
}

// runLint implements `mio lint`.
func runLint(args []string, stdout, stderr io.Writer) int {
	options := cliDiagnosticOptions{color: diagnostic.ColorNever, format: diagnostic.FormatHuman}
	denyWarnings := false
	path := ""
	for _, argument := range args {
		switch {
		case argument == "-h" || argument == "--help":
			fmt.Fprint(stdout, lintUsage)
			return 0
		case argument == "--list-rules":
			for _, name := range lint.RuleNames {
				fmt.Fprintf(stdout, "%-18s %s\n", name, lintRuleDescriptions[name])
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
			fmt.Fprint(stderr, lintUsage)
			return 2
		default:
			path = argument
		}
	}
	if path == "" {
		fmt.Fprint(stderr, lintUsage)
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
			fmt.Fprintf(stdout, "%s:%d:%d: warning[%s]: %s\n", f.File, f.Line, f.Column, f.Rule, f.Message)
		}
	}
	if denyWarnings && len(findings) > 0 {
		return 1
	}
	return 0
}
