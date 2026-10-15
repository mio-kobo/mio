package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Mio/internal/diagnostic"
	"Mio/internal/i18n"
	"Mio/internal/tester"
	"Mio/internal/vm"
)

type testCommandOptions struct {
	target string
	run    string
	lang   i18n.Language
	diag   cliDiagnosticOptions
}

type jsonTestFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type jsonTestError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Stack   []vm.RuntimeFrame `json:"stack,omitempty"`
}

type jsonTestResult struct {
	Name       string            `json:"name"`
	Path       string            `json:"path"`
	Status     string            `json:"status"`
	DurationMS float64           `json:"duration_ms"`
	Failures   []jsonTestFailure `json:"failures,omitempty"`
	Error      *jsonTestError    `json:"error,omitempty"`
}

type jsonPackageResult struct {
	Path   string           `json:"path"`
	Status string           `json:"status"`
	Tests  []jsonTestResult `json:"tests"`
	Error  *jsonTestError   `json:"error,omitempty"`
}

type jsonTestSummary struct {
	Status     string  `json:"status"`
	Passed     int     `json:"passed"`
	Failed     int     `json:"failed"`
	DurationMS float64 `json:"duration_ms"`
}

type jsonTestReport struct {
	SchemaVersion int                 `json:"schema_version"`
	Packages      []jsonPackageResult `json:"packages"`
	Summary       jsonTestSummary     `json:"summary"`
}

func runTestCommand(args []string, stdout, stderr io.Writer) int {
	options, ok := parseTestArgs(args)
	if !ok {
		printUsage(stderr)
		return 2
	}
	packages, err := discoverTestPackages(options.target)
	if err != nil {
		renderTestCommandError(stderr, options, options.target, err)
		return 1
	}
	started := time.Now()
	report := jsonTestReport{SchemaVersion: 1}
	discovered := 0
	for _, packagePath := range packages {
		if options.diag.format != diagnostic.FormatJSON {
			fmt.Fprintf(stdout, i18n.Text(options.lang, i18n.TestStarting)+"\n", packagePath)
		}
		packageResult, runErr := tester.RunPackage(packagePath, bunkoRoot(packagePath), tester.Options{Output: stdout, RunPattern: options.run})
		if runErr != nil {
			failedPackage := jsonPackageResult{
				Path: packagePath, Status: "compile_failed",
				Tests: []jsonTestResult{}, Error: compileJSONError(runErr),
			}
			var testCompileErr *tester.CompileError
			if errors.As(runErr, &testCompileErr) {
				failedPackage.Tests = append(failedPackage.Tests, jsonTestResult{
					Name: testCompileErr.TestName, Path: testCompileErr.Path, Status: "compile_failed",
					Error: compileJSONError(runErr),
				})
			}
			report.Packages = append(report.Packages, failedPackage)
			report.Summary.Failed++
			if options.diag.format != diagnostic.FormatJSON {
				renderTestCommandError(stderr, options, packagePath, runErr)
			}
			continue
		}
		discovered += packageResult.Discovered
		jsonPackage := jsonPackageResult{Path: packagePath, Status: "passed", Tests: []jsonTestResult{}}
		for _, result := range packageResult.Tests {
			jsonResult := makeJSONTestResult(result)
			jsonPackage.Tests = append(jsonPackage.Tests, jsonResult)
			if result.Passed {
				report.Summary.Passed++
			} else {
				report.Summary.Failed++
				jsonPackage.Status = "failed"
			}
			if options.diag.format != diagnostic.FormatJSON {
				if options.diag.format == diagnostic.FormatShort {
					renderShortTestResult(stdout, options.lang, packagePath, result)
				} else {
					renderHumanTestResult(stdout, options.lang, result)
				}
			}
		}
		report.Packages = append(report.Packages, jsonPackage)
	}
	report.Summary.DurationMS = durationMilliseconds(time.Since(started))
	if report.Summary.Failed != 0 {
		report.Summary.Status = "failed"
	} else if report.Summary.Passed == 0 {
		report.Summary.Status = "no_tests"
	} else {
		report.Summary.Status = "passed"
	}
	if options.diag.format == diagnostic.FormatJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(stderr, "mio:", err)
			return 1
		}
	} else {
		if report.Summary.Passed == 0 && report.Summary.Failed == 0 {
			if options.run != "" && discovered != 0 {
				fmt.Fprintf(stdout, i18n.Text(options.lang, i18n.NoMatchingTests)+"\n", options.run)
			} else {
				fmt.Fprintln(stdout, i18n.Text(options.lang, i18n.NoTests))
			}
		}
		fmt.Fprintf(stdout, i18n.Text(options.lang, i18n.Summary)+"\n", report.Summary.Passed, report.Summary.Failed)
		fmt.Fprintf(stdout, "%s: %.3fs\n", i18n.Text(options.lang, i18n.TotalDuration), time.Since(started).Seconds())
	}
	if report.Summary.Failed != 0 {
		return 1
	}
	return 0
}

func parseTestArgs(args []string) (testCommandOptions, bool) {
	options := testCommandOptions{diag: cliDiagnosticOptions{color: diagnostic.ColorAuto, format: diagnostic.FormatHuman}}
	explicitLanguage := ""
	for _, arg := range args {
		switch arg {
		case "--color=auto":
			options.diag.color = diagnostic.ColorAuto
		case "--color=always":
			options.diag.color = diagnostic.ColorAlways
		case "--color=never":
			options.diag.color = diagnostic.ColorNever
		case "--format=human":
			options.diag.format = diagnostic.FormatHuman
		case "--format=short":
			options.diag.format = diagnostic.FormatShort
		case "--format=json":
			options.diag.format = diagnostic.FormatJSON
		case "--verbose":
			options.diag.verbose = true
		default:
			if strings.HasPrefix(arg, "--run=") {
				options.run = strings.TrimPrefix(arg, "--run=")
				if options.run == "" {
					return testCommandOptions{}, false
				}
				continue
			}
			if strings.HasPrefix(arg, "--lang=") {
				explicitLanguage = strings.TrimPrefix(arg, "--lang=")
				if explicitLanguage == "" {
					return testCommandOptions{}, false
				}
				continue
			}
			if strings.HasPrefix(arg, "--") || options.target != "" || arg == "" {
				return testCommandOptions{}, false
			}
			options.target = arg
		}
	}
	if options.target == "" {
		return testCommandOptions{}, false
	}
	options.lang = i18n.Select(explicitLanguage, os.Getenv)
	return options, true
}

func discoverTestPackages(target string) ([]string, error) {
	if !strings.HasSuffix(filepath.ToSlash(target), "/...") && target != "..." {
		info, err := os.Stat(target)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("test target %q is not a directory", target)
		}
		return []string{filepath.Clean(target)}, nil
	}
	root := strings.TrimSuffix(filepath.ToSlash(target), "/...")
	if root == "" || root == "." {
		root = "."
	}
	var packages []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && excludedTestDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.mio") {
			directory := filepath.Clean(filepath.Dir(path))
			if len(packages) == 0 || packages[len(packages)-1] != directory {
				packages = append(packages, directory)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(packages)
	return packages, nil
}

func excludedTestDirectory(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "build", "dist", "node_modules", "vendor", "cache", "tmp":
		return true
	default:
		return false
	}
}

func renderHumanTestResult(out io.Writer, language i18n.Language, result tester.Result) {
	status := i18n.Text(language, i18n.TestPassed)
	if !result.Passed {
		status = i18n.Text(language, i18n.TestFailed)
	}
	fmt.Fprintf(out, "--- %s: %s (%.3fs)\n", status, result.Name, result.Duration.Seconds())
	for _, failure := range result.Failures {
		fmt.Fprintf(out, "    %s: %s\n", i18n.Text(language, i18n.AssertionFailed), failure.Message)
	}
	if result.RuntimeErr != nil {
		label := i18n.Text(language, i18n.RuntimeFailed)
		if result.Resource {
			label = i18n.Text(language, i18n.ResourceLimit)
		}
		fmt.Fprintf(out, "    %s: %v\n", label, result.RuntimeErr)
	}
}

func renderShortTestResult(out io.Writer, language i18n.Language, packagePath string, result tester.Result) {
	status := i18n.Text(language, i18n.TestPassed)
	if !result.Passed {
		status = i18n.Text(language, i18n.TestFailed)
	}
	fmt.Fprintf(out, "%s:%s: %s (%.3fs)\n", packagePath, result.Name, status, result.Duration.Seconds())
	for _, failure := range result.Failures {
		fmt.Fprintf(out, "%s:%s: E0602: %s\n", packagePath, result.Name, failure.Message)
	}
	if result.RuntimeErr != nil {
		code := "E0603"
		if result.Resource {
			code = "E0604"
		}
		fmt.Fprintf(out, "%s:%s: %s: %v\n", packagePath, result.Name, code, result.RuntimeErr)
	}
}

func renderTestCommandError(out io.Writer, options testCommandOptions, path string, err error) {
	var d *diagnostic.Diagnostic
	if errors.As(err, &d) {
		localized := *d
		localized.Labels = append([]diagnostic.Label(nil), d.Labels...)
		localized.Notes = append([]diagnostic.Note(nil), d.Notes...)
		localized.Helps = append([]diagnostic.Help(nil), d.Helps...)
		if d.Code == "E0601" {
			localized.Message = i18n.Text(options.lang, i18n.InvalidTestSignature)
		} else {
			localized.Notes = append([]diagnostic.Note{{Message: d.Message}}, localized.Notes...)
			localized.Message = i18n.Text(options.lang, i18n.CompileFailed)
		}
		renderPath := localized.Path
		if renderPath == "" {
			renderPath = path
		}
		renderCLIError(out, renderPath, &localized, options.diag, false)
		return
	}
	fmt.Fprintf(out, "%s: %v\n", i18n.Text(options.lang, i18n.CompileFailed), err)
}

func makeJSONTestResult(result tester.Result) jsonTestResult {
	status := "passed"
	if !result.Passed {
		status = "failed"
	}
	encoded := jsonTestResult{Name: result.Name, Path: result.Path, Status: status, DurationMS: durationMilliseconds(result.Duration)}
	for _, failure := range result.Failures {
		encoded.Failures = append(encoded.Failures, jsonTestFailure{Code: "E0602", Message: failure.Message})
	}
	if result.RuntimeErr != nil {
		code := "E0603"
		if result.Resource {
			code = "E0604"
		}
		encoded.Error = &jsonTestError{Code: code, Message: result.RuntimeErr.Error()}
		var runtimeErr *vm.RuntimeError
		if errors.As(result.RuntimeErr, &runtimeErr) {
			encoded.Error.Stack = runtimeErr.Stack
		}
	}
	return encoded
}

func compileJSONError(err error) *jsonTestError {
	code := "E0605"
	var d *diagnostic.Diagnostic
	if errors.As(err, &d) && d.Code != "" {
		code = d.Code
	}
	return &jsonTestError{Code: code, Message: err.Error()}
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
