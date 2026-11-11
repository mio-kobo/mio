package bunkoscaffold

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"Mio/internal/bunkopm"
	"Mio/internal/i18n"
)

type Context struct {
	In                   io.Reader
	Out, Err             io.Writer
	Language             i18n.Language
	Root, Compiler, Host string
	Paint                func(string, string) string
	Runner               Runner
	reader               *bufio.Reader
}

// Keep dynamic prompt/error suffixes in a closed, statically auditable catalog.
var scaffoldMessages = map[string]i18n.Key{
	"action_connect":        i18n.KeyScaffoldActionConnect,
	"action_git_config":     i18n.KeyScaffoldActionGitConfig,
	"action_git_init":       i18n.KeyScaffoldActionGitInit,
	"action_login":          i18n.KeyScaffoldActionLogin,
	"action_repo_create":    i18n.KeyScaffoldActionRepoCreate,
	"active_account":        i18n.KeyScaffoldActiveAccount,
	"auth":                  i18n.KeyScaffoldAuth,
	"cancelled":             i18n.KeyScaffoldCancelled,
	"cmd_connect":           i18n.KeyScaffoldCmdConnect,
	"cmd_create":            i18n.KeyScaffoldCmdCreate,
	"connected":             i18n.KeyScaffoldConnected,
	"created":               i18n.KeyScaffoldCreated,
	"declined":              i18n.KeyScaffoldDeclined,
	"environment":           i18n.KeyScaffoldEnvironment,
	"exists":                i18n.KeyScaffoldExists,
	"git_config":            i18n.KeyScaffoldGitConfig,
	"help_connect":          i18n.KeyScaffoldHelpConnect,
	"help_create":           i18n.KeyScaffoldHelpCreate,
	"interactive_timeout":   i18n.KeyScaffoldInteractiveTimeout,
	"invalid_dependency":    i18n.KeyScaffoldInvalidDependency,
	"invalid_description":   i18n.KeyScaffoldInvalidDescription,
	"invalid_license":       i18n.KeyScaffoldInvalidLicense,
	"invalid_name":          i18n.KeyScaffoldInvalidName,
	"invalid_path":          i18n.KeyScaffoldInvalidPath,
	"invalid_platform":      i18n.KeyScaffoldInvalidPlatform,
	"invalid_version":       i18n.KeyScaffoldInvalidVersion,
	"io":                    i18n.KeyScaffoldIo,
	"local_only":            i18n.KeyScaffoldLocalOnly,
	"missing":               i18n.KeyScaffoldMissing,
	"missing_gh":            i18n.KeyScaffoldMissingGh,
	"missing_git":           i18n.KeyScaffoldMissingGit,
	"multiple_accounts":     i18n.KeyScaffoldMultipleAccounts,
	"namespace_auth":        i18n.KeyScaffoldNamespaceAuth,
	"native_process":        i18n.KeyScaffoldNativeProcess,
	"not_package":           i18n.KeyScaffoldNotPackage,
	"options":               i18n.KeyScaffoldOptions,
	"platform_choices":      i18n.KeyScaffoldPlatformChoices,
	"preview":               i18n.KeyScaffoldPreview,
	"proceed":               i18n.KeyScaffoldProceed,
	"process":               i18n.KeyScaffoldProcess,
	"prompt_create":         i18n.KeyScaffoldPromptCreate,
	"prompt_dependencies":   i18n.KeyScaffoldPromptDependencies,
	"prompt_description":    i18n.KeyScaffoldPromptDescription,
	"prompt_github":         i18n.KeyScaffoldPromptGithub,
	"prompt_license":        i18n.KeyScaffoldPromptLicense,
	"prompt_name":           i18n.KeyScaffoldPromptName,
	"prompt_namespace":      i18n.KeyScaffoldPromptNamespace,
	"prompt_platforms":      i18n.KeyScaffoldPromptPlatforms,
	"prompt_repo":           i18n.KeyScaffoldPromptRepo,
	"prompt_version":        i18n.KeyScaffoldPromptVersion,
	"prompt_visibility":     i18n.KeyScaffoldPromptVisibility,
	"prompt_workflow":       i18n.KeyScaffoldPromptWorkflow,
	"remote_preview":        i18n.KeyScaffoldRemotePreview,
	"repository_create":     i18n.KeyScaffoldRepositoryCreate,
	"repository_created":    i18n.KeyScaffoldRepositoryCreated,
	"repository_permission": i18n.KeyScaffoldRepositoryPermission,
	"stdin":                 i18n.KeyScaffoldStdin,
	"timeout":               i18n.KeyScaffoldTimeout,
	"tool":                  i18n.KeyScaffoldTool,
	"workflow_dependencies": i18n.KeyScaffoldWorkflowDependencies,
}

func (c *Context) text(key string, args ...string) string {
	values := make([]any, len(args))
	for i, v := range args {
		values[i] = v
	}
	return i18n.Format(c.Language, scaffoldMessages[key], values...)
}
func (c *Context) print(key string, args ...string) { fmt.Fprintln(c.Out, c.text(key, args...)) }
func (c *Context) line(key string) (string, error) {
	prompt := c.text(key) + " "
	if c.Paint != nil {
		prompt = c.Paint("heading", prompt)
	}
	fmt.Fprint(c.Out, prompt)
	var b strings.Builder
	for {
		part, e := c.reader.ReadSlice('\n')
		if b.Len()+len(part) > 8192 {
			return "", invalid("stdin", "")
		}
		b.Write(part)
		if errors.Is(e, bufio.ErrBufferFull) {
			continue
		}
		if e != nil && b.Len() == 0 {
			return "", invalid("stdin", "")
		}
		if e != nil && e != io.EOF {
			return "", invalid("stdin", "")
		}
		return strings.TrimSpace(b.String()), nil
	}
}
func (c *Context) yes(key string) bool {
	v, e := c.line(key)
	return e == nil && (strings.EqualFold(v, "y") || strings.EqualFold(v, "yes"))
}

type repeat []string

func (r *repeat) String() string     { return strings.Join(*r, ",") }
func (r *repeat) Set(s string) error { *r = append(*r, s); return nil }

type Options struct {
	Spec                                                                    Spec
	NonInteractive, GitInit, Github, NewRepo                                bool
	Repo, Visibility, AllowedAccount                                        string
	AllowGitInit, AllowGitConfig, AllowConnect, AllowRepoCreate, AllowLogin bool
}

func Parse(command string, args []string) (Options, error) {
	var o Options
	var targets, deps repeat
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.BoolVar(&o.NonInteractive, "non-interactive", false, "")
	f.StringVar(&o.Spec.Namespace, "namespace", "", "")
	f.StringVar(&o.Spec.Name, "name", "", "")
	f.StringVar(&o.Spec.Description, "description", "", "")
	f.StringVar(&o.Spec.Version, "version", "", "")
	f.StringVar(&o.Spec.License, "license", "", "")
	f.StringVar(&o.Spec.Directory, "directory", "", "")
	f.Var(&targets, "platform", "")
	f.Var(&deps, "dependency", "")
	f.BoolVar(&o.Spec.Workflow, "workflow", false, "")
	f.BoolVar(&o.GitInit, "git-init", false, "")
	f.BoolVar(&o.Github, "github", false, "")
	f.StringVar(&o.Repo, "repo", "", "")
	f.BoolVar(&o.NewRepo, "create-repo", false, "")
	f.StringVar(&o.Visibility, "visibility", "", "")
	f.StringVar(&o.AllowedAccount, "allow-account", "", "")
	f.BoolVar(&o.AllowGitInit, "allow-git-init", false, "")
	f.BoolVar(&o.AllowGitConfig, "allow-git-config", false, "")
	f.BoolVar(&o.AllowConnect, "allow-connect", false, "")
	f.BoolVar(&o.AllowRepoCreate, "allow-repo-create", false, "")
	f.BoolVar(&o.AllowLogin, "allow-auth-login", false, "")
	if e := f.Parse(args); e != nil || len(f.Args()) > 0 {
		return o, invalid("options", "")
	}
	o.Spec.Dependencies = map[string]string{}
	for _, v := range targets {
		o.Spec.Targets = append(o.Spec.Targets, split(v)...)
	}
	for _, v := range deps {
		for _, spec := range split(v) {
			n, version, e := bunkopm.ParseSpec(spec)
			if e != nil {
				return o, invalid("invalid_dependency", spec)
			}
			if old, ok := o.Spec.Dependencies[n]; ok && old != version {
				return o, invalid("invalid_dependency", n)
			}
			o.Spec.Dependencies[n] = version
		}
	}
	if o.Visibility != "" && o.Visibility != "public" && o.Visibility != "private" {
		return o, invalid("options", "")
	}
	if command == "create" && (o.NewRepo || o.Repo != "") {
		o.Github = true
	}
	return o, nil
}
func split(s string) []string {
	var a []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			a = append(a, p)
		}
	}
	return a
}
func (c *Context) permission(id, preview string, o Options) bool {
	fmt.Fprintln(c.Out, bunkopm.SafeText(preview))
	allowed := false
	switch id {
	case "git_init":
		allowed = o.AllowGitInit
	case "git_config":
		allowed = o.AllowGitConfig
	case "connect":
		allowed = o.AllowConnect
	case "repo_create":
		allowed = o.AllowRepoCreate
	case "login":
		allowed = o.AllowLogin
	case "account":
		allowed = o.AllowedAccount != ""
	}
	if !o.NonInteractive {
		allowed = c.yes("proceed")
	}
	if !allowed {
		c.print("declined")
	}
	return allowed
}
func Run(command string, args []string, c Context) int {
	if c.In == nil {
		c.In = strings.NewReader("")
	}
	if c.Out == nil {
		c.Out = io.Discard
	}
	if c.Err == nil {
		c.Err = io.Discard
	}
	if c.Runner == nil {
		c.Runner = OSRunner{}
	}
	if c.Root == "" {
		c.Root = "."
	}
	c.reader = bufio.NewReader(c.In)
	if wantsHelp(args) {
		if c.Paint != nil {
			fmt.Fprintln(c.Out, c.Paint("heading", "mio bunko "+command))
		}
		c.print("help_" + command)
		return 0
	}

	o, e := Parse(command, args)
	if e != nil {
		return c.failure(e)
	}
	env := detect(c.Runner)
	c.print("environment", bunkopm.SafeText(c.Compiler), bunkopm.SafeText(c.Host))
	for _, entry := range []struct {
		name string
		tool Tool
	}{{"Git", env.Git}, {"gh", env.Gh}} {
		value := entry.tool.Version
		if entry.tool.Path == "" {
			value = c.text("missing")
		}
		c.print("tool", entry.name, bunkopm.SafeText(value))
	}
	if env.Gh.Path == "" {
		c.print("missing_gh")
	}
	if command == "connect" {
		e = c.connect(c.Root, o, env)
		if isDeclined(e) {
			return 0
		}
		if e != nil {
			return c.failure(e)
		}
		return 0
	}
	if command != "create" {
		return c.failure(invalid("options", ""))
	}
	if !o.NonInteractive {
		for _, p := range []struct {
			field *string
			key   string
		}{{&o.Spec.Namespace, "namespace"}, {&o.Spec.Name, "name"}, {&o.Spec.Description, "description"}, {&o.Spec.Version, "version"}, {&o.Spec.License, "license"}} {
			if *p.field == "" {
				*p.field, e = c.line("prompt_" + p.key)
				if e != nil {
					return c.failure(e)
				}
			}
		}
		if len(o.Spec.Targets) == 0 {
			c.print("platform_choices", strings.Join(Targets(), ", "))
			v, e := c.line("prompt_platforms")
			if e != nil {
				return c.failure(e)
			}
			o.Spec.Targets = split(v)
		}
		if len(o.Spec.Dependencies) == 0 {
			v, e := c.line("prompt_dependencies")
			if e != nil {
				return c.failure(e)
			}
			if v != "" {
				parsed, e := Parse("create", []string{"--dependency", v})
				if e != nil {
					return c.failure(e)
				}
				o.Spec.Dependencies = parsed.Spec.Dependencies
			}
		}
		if !o.Spec.Workflow {
			o.Spec.Workflow = c.yes("prompt_workflow")
		}
	}
	if o.Spec.Version == "" {
		o.Spec.Version = "0.1.0"
	}
	if o.Spec.License == "" {
		o.Spec.License = "MIT"
	}
	if e = o.Spec.Validate(); e != nil {
		return c.failure(e)
	}
	c.print("preview", o.Spec.FullName()+"@"+o.Spec.Version, o.Spec.Directory, strings.Join(o.Spec.Targets, ", "))
	if !o.NonInteractive && !c.yes("prompt_create") {
		c.print("cancelled")
		return 0
	}
	dir, e := Generate(c.Root, o.Spec)
	if e != nil {
		return c.failure(e)
	}
	c.print("created", bunkopm.SafeText(dir))
	if o.GitInit || (!o.NonInteractive && env.Git.Path != "") {
		if env.Git.Path == "" {
			return c.failure(invalid("missing_git", ""))
		}
		if c.permission("git_init", c.text("action_git_init", dir), o) {
			if e = gitInit(c.Runner, env.Git.Path, dir); e != nil {
				return c.failure(e)
			}
		}
	}
	if !o.Github && !o.NonInteractive && env.Gh.Path != "" {
		o.Github = c.yes("prompt_github")
	}
	if o.Github {
		if e = c.connect(dir, o, env); e != nil && !isDeclined(e) {
			return c.failure(e)
		}
	} else {
		c.print("local_only")
	}
	return 0
}
func (c *Context) failure(e error) int {
	kind, detail := "io", e.Error()
	var own *Error
	if errors.As(e, &own) {
		kind = own.Kind
		detail = own.Detail
	}
	fmt.Fprintln(c.Err, "BUNKO_E_SCAFFOLD_"+strings.ToUpper(kind), c.text(kind, bunkopm.SafeText(detail)))
	if kind == "options" {
		return 2
	}
	return 1
}

// TakesValue also protects literal option-looking descriptions from the global
// language/theme parser. --version is scoped to create by the CLI adapter.
func TakesValue(name string) bool {
	switch name {
	case "--namespace", "--name", "--description", "--version", "--license", "--directory", "--platform", "--dependency", "--repo", "--visibility", "--allow-account":
		return true
	}
	return false
}
func wantsHelp(args []string) bool {
	for at := 0; at < len(args); at++ {
		if args[at] == "--help" || args[at] == "-h" {
			return true
		}
		if TakesValue(args[at]) && at+1 < len(args) {
			at++
		}
	}
	return false
}
