package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"Mio/internal/bunkopm"
	"Mio/internal/i18n"
	"github.com/BurntSushi/toml"
)

type bunkoCommand struct {
	name, syntax string
	key          i18n.Key
}

func bunkoCommands() []bunkoCommand {
	return []bunkoCommand{
		{"create", "create [options]", i18n.KeyScaffoldCmdCreate}, {"connect", "connect [options]", i18n.KeyScaffoldCmdConnect},
		{"init", "init", i18n.KeyBunkoInitHelp}, {"add", "add <namespace/package@version>", i18n.KeyBunkoAddHelp},
		{"remove", "remove <namespace/package>", i18n.KeyBunkoRemoveHelp}, {"install", "install", i18n.KeyBunkoInstallHelp},
		{"list", "list", i18n.KeyBunkoListHelp}, {"info", "info <namespace/package>", i18n.KeyBunkoInfoHelp},
		{"search", "search <query>", i18n.KeyBunkoSearchHelp},
	}
}
func runBunko(args []string, out, stderr io.Writer, lang i18n.Language, theme helpTheme) int {
	if len(args) > 0 && (args[0] == "create" || args[0] == "connect") {
		return runBunkoScaffold(args, os.Stdin, out, stderr, lang, theme)
	}
	help := len(args) == 0
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			help = true
		}
	}
	if help {
		printCommandHelp(out, lang, theme, "bunko")
	}
	return runBunkoWithFetcher(args, out, stderr, lang, bunkopm.NewFetcher())
}
func runBunkoWithFetcher(args []string, out, stderr io.Writer, lang i18n.Language, fetch *bunkopm.Fetcher) int {
	help := len(args) == 0
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			help = true
		}
	}
	if help {
		fmt.Fprintln(out, i18n.Text(lang, i18n.KeyBunkoCliHelp))
		for _, cmd := range bunkoCommands() {
			if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == cmd.name {
				fmt.Fprintf(out, "mio bunko %s\n  %s\n", cmd.syntax, i18n.Text(lang, cmd.key))
			}
		}
		return 0
	}
	known := false
	for _, cmd := range bunkoCommands() {
		if args[0] == cmd.name {
			known = true
		}
	}
	arity := 1
	if args[0] == "add" || args[0] == "remove" || args[0] == "info" || args[0] == "search" {
		arity = 2
	}
	if !known || len(args) != arity {
		fmt.Fprintln(stderr, "BUNKO_E_OPTIONS", i18n.Text(lang, i18n.KeyBunkoUsageError))
		return 2
	}
	dir, e := os.Getwd()
	if e != nil {
		return bunkoFailure(stderr, lang, e)
	}
	switch args[0] {
	case "init":
		e = bunkopm.Init(dir)
	case "remove":
		e = bunkopm.Remove(dir, args[1])
	case "list":
		var m bunkopm.Manifest
		m, e = bunkopm.ReadManifest(dir)
		if e != nil {
			break
		}
		cache, err := bunkopm.CacheRoot()
		if err != nil {
			e = err
			break
		}
		idx, err := bunkopm.OpenCachedIndex(cache)
		for _, n := range bunkopm.Sorted(m.Dependencies) {
			status := i18n.Text(lang, i18n.KeyBunkoMissingInstall)
			if err == nil {
				r, err := idx.Record(n, m.Dependencies[n])
				if err == nil {
					if _, err = bunkopm.VerifyCached(cache, r); err == nil {
						status = i18n.Text(lang, i18n.KeyBunkoInstalled)
					}
				}
			}
			fmt.Fprintf(out, "%s@%s  %s\n", n, m.Dependencies[n], status)
		}
		return 0
	default:
		// Validate local input before making any network request.
		if args[0] == "add" {
			if _, _, err := bunkopm.ParseSpec(args[1]); err != nil {
				return bunkoFailure(stderr, lang, err)
			}
		}
		if args[0] == "info" && !bunkopm.ValidName(args[1]) {
			return bunkoFailure(stderr, lang, &bunkopm.Error{Code: "BUNKO_E_SPEC", Detail: args[1]})
		}
		if args[0] == "install" || args[0] == "add" {
			if _, err := bunkopm.ReadManifest(dir); err != nil {
				return bunkoFailure(stderr, lang, err)
			}
		}
		f := fetch
		idx, err := bunkopm.OpenIndex(f)
		if err != nil {
			return bunkoFailure(stderr, lang, err)
		}
		cache, err := bunkopm.CacheRoot()
		if err != nil {
			return bunkoFailure(stderr, lang, err)
		}
		// Refresh revocations even when a subsequently requested package is unavailable.
		if err = idx.SavePolicy(cache); err != nil {
			return bunkoFailure(stderr, lang, err)
		}
		c := bunkopm.Client{Index: idx, Cache: cache, Compiler: strings.Split(strings.TrimPrefix(version, "v"), "-")[0], Target: bunkopm.Target(runtime.GOOS, runtime.GOARCH), Fetch: f}
		switch args[0] {
		case "add":
			e = c.Add(dir, args[1])
		case "install":
			e = c.Install(dir)
		case "info":
			vs := idx.Versions(args[1])
			if len(vs) == 0 {
				return bunkoFailure(stderr, lang, &bunkopm.Error{Code: "BUNKO_E_MISSING_PACKAGE", Detail: args[1]})
			}
			for _, v := range vs {
				r, err := idx.Record(args[1], v)
				if err != nil {
					var be *bunkopm.Error
					if errors.As(err, &be) && be.Code == "BUNKO_E_REVOKED" {
						fmt.Fprintln(out, bunkopm.SafeText(err.Error()))
						continue
					}
					return bunkoFailure(stderr, lang, err)
				}
				r.Description = bunkopm.SafeText(r.Description)
				var b strings.Builder
				if e = toml.NewEncoder(&b).Encode(r); e != nil {
					break
				}
				fmt.Fprintln(out, b.String())
			}
			return 0
		case "search":
			query := strings.ToLower(args[1])
			for _, n := range idx.Names() {
				vs := idx.Versions(n)
				for at := len(vs) - 1; at >= 0; at-- {
					r, err := idx.Record(n, vs[at])
					if err != nil {
						var be *bunkopm.Error
						if errors.As(err, &be) && be.Code == "BUNKO_E_REVOKED" {
							continue
						}
						return bunkoFailure(stderr, lang, err)
					}
					if strings.Contains(n, query) || strings.Contains(strings.ToLower(r.Description), query) {
						fmt.Fprintf(out, "%s@%s  %s\n", n, r.Version, bunkopm.SafeText(r.Description))
					}
					break
				}
			}
			return 0
		}
	}
	if e != nil {
		return bunkoFailure(stderr, lang, e)
	}
	fmt.Fprintln(out, i18n.Text(lang, i18n.KeyBunkoSuccess))
	return 0
}
func bunkoFailure(w io.Writer, lang i18n.Language, e error) int {
	code := "BUNKO_E_IO"
	detail := e.Error()
	var be *bunkopm.Error
	if errors.As(e, &be) {
		code = be.Code
		detail = be.Detail
	}
	fmt.Fprintln(w, code, i18n.Format(lang, i18n.KeyBunkoFailure, bunkopm.SafeText(detail)))
	return 1
}
