package bunkoscaffold

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	"non_tty":                 i18n.KeyScaffoldNonTty,
	"publish_guide":           i18n.KeyScaffoldPublishGuide,
	"next":                    i18n.KeyScaffoldNext,
	"location":                i18n.KeyScaffoldLocation,
	"summary_dependencies":    i18n.KeyScaffoldSummaryDependencies,
	"summary":                 i18n.KeyScaffoldSummary,
	"numbers":                 i18n.KeyScaffoldNumbers,
	"keys":                    i18n.KeyScaffoldKeys,
	"platform_windows_x86_64": i18n.KeyScaffoldPlatformWindowsX8664,
	"platform_macos_arm64":    i18n.KeyScaffoldPlatformMacosArm64,
	"platform_linux_x86_64":   i18n.KeyScaffoldPlatformLinuxX8664,
	"current_device":          i18n.KeyScaffoldCurrentDevice,
	"select_platforms":        i18n.KeyScaffoldSelectPlatforms,
	"prompt_full_name":        i18n.KeyScaffoldPromptFullName,
	"welcome":                 i18n.KeyScaffoldWelcome,
	"action_connect":          i18n.KeyScaffoldActionConnect,
	"action_git_config":       i18n.KeyScaffoldActionGitConfig,
	"action_git_init":         i18n.KeyScaffoldActionGitInit,
	"action_login":            i18n.KeyScaffoldActionLogin,
	"action_repo_create":      i18n.KeyScaffoldActionRepoCreate,
	"active_account":          i18n.KeyScaffoldActiveAccount,
	"auth":                    i18n.KeyScaffoldAuth,
	"cancelled":               i18n.KeyScaffoldCancelled,
	"cmd_connect":             i18n.KeyScaffoldCmdConnect,
	"cmd_create":              i18n.KeyScaffoldCmdCreate,
	"connected":               i18n.KeyScaffoldConnected,
	"created":                 i18n.KeyScaffoldCreated,
	"declined":                i18n.KeyScaffoldDeclined,
	"environment":             i18n.KeyScaffoldEnvironment,
	"exists":                  i18n.KeyScaffoldExists,
	"git_config":              i18n.KeyScaffoldGitConfig,
	"help_connect":            i18n.KeyScaffoldHelpConnect,
	"help_create":             i18n.KeyScaffoldHelpCreate,
	"interactive_timeout":     i18n.KeyScaffoldInteractiveTimeout,
	"invalid_dependency":      i18n.KeyScaffoldInvalidDependency,
	"invalid_description":     i18n.KeyScaffoldInvalidDescription,
	"invalid_license":         i18n.KeyScaffoldInvalidLicense,
	"invalid_name":            i18n.KeyScaffoldInvalidName,
	"invalid_path":            i18n.KeyScaffoldInvalidPath,
	"invalid_platform":        i18n.KeyScaffoldInvalidPlatform,
	"invalid_version":         i18n.KeyScaffoldInvalidVersion,
	"io":                      i18n.KeyScaffoldIo,
	"local_only":              i18n.KeyScaffoldLocalOnly,
	"missing":                 i18n.KeyScaffoldMissing,
	"missing_gh":              i18n.KeyScaffoldMissingGh,
	"missing_git":             i18n.KeyScaffoldMissingGit,
	"multiple_accounts":       i18n.KeyScaffoldMultipleAccounts,
	"namespace_auth":          i18n.KeyScaffoldNamespaceAuth,
	"native_process":          i18n.KeyScaffoldNativeProcess,
	"not_package":             i18n.KeyScaffoldNotPackage,
	"options":                 i18n.KeyScaffoldOptions,
	"platform_choices":        i18n.KeyScaffoldPlatformChoices,
	"preview":                 i18n.KeyScaffoldPreview,
	"proceed":                 i18n.KeyScaffoldProceed,
	"process":                 i18n.KeyScaffoldProcess,
	"prompt_create":           i18n.KeyScaffoldPromptCreate,
	"prompt_dependencies":     i18n.KeyScaffoldPromptDependencies,
	"prompt_description":      i18n.KeyScaffoldPromptDescription,
	"prompt_github":           i18n.KeyScaffoldPromptGithub,
	"prompt_license":          i18n.KeyScaffoldPromptLicense,
	"prompt_name":             i18n.KeyScaffoldPromptName,
	"prompt_namespace":        i18n.KeyScaffoldPromptNamespace,
	"prompt_platforms":        i18n.KeyScaffoldPromptPlatforms,
	"prompt_repo":             i18n.KeyScaffoldPromptRepo,
	"prompt_version":          i18n.KeyScaffoldPromptVersion,
	"prompt_visibility":       i18n.KeyScaffoldPromptVisibility,
	"prompt_workflow":         i18n.KeyScaffoldPromptWorkflow,
	"remote_preview":          i18n.KeyScaffoldRemotePreview,
	"repository_create":       i18n.KeyScaffoldRepositoryCreate,
	"repository_created":      i18n.KeyScaffoldRepositoryCreated,
	"repository_permission":   i18n.KeyScaffoldRepositoryPermission,
	"stdin":                   i18n.KeyScaffoldStdin,
	"timeout":                 i18n.KeyScaffoldTimeout,
	"tool":                    i18n.KeyScaffoldTool,
	"workflow_dependencies":   i18n.KeyScaffoldWorkflowDependencies,
}

func (c *Context) text(key string, args ...string) string {
	values := make([]any, len(args))
	for i, v := range args {
		values[i] = v
	}
	return i18n.Format(c.Language, scaffoldMessages[key], values...)
}
func (c *Context) print(key string, args ...string) {
	text := c.text(key, args...)
	if c.Paint != nil && (key == "created" || key == "welcome" || key == "select_platforms" || key == "prompt_platforms") {
		role := "command"
		if key == "created" {
			role = "success"
		}
		text = c.Paint(role, text)
	}
	fmt.Fprintln(c.Out, text)
}
func (c *Context) line(key string) (string, error) {
	prompt := c.text(key) + "\n› "
	if c.Paint != nil {
		prompt = c.Paint("command", prompt)
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
	Advanced                                                                bool
	NonInteractive, GitInit, Github, NewRepo                                bool
	Repo, Visibility, AllowedAccount                                        string
	AllowGitInit, AllowGitConfig, AllowConnect, AllowRepoCreate, AllowLogin bool
}

func Parse(command string, args []string) (Options, error) {
	var o Options
	var targets, deps repeat
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.BoolVar(&o.Advanced, "advanced", false, "")
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
			fmt.Fprintln(c.Out, c.Paint("command", "mio bunko "+command))
		}
		c.print("help_" + command)
		return 0
	}

	o, e := Parse(command, args)
	if e != nil {
		return c.failure(e)
	}
	var env Environment
	if command == "connect" || o.GitInit || o.Github {
		env = detect(c.Runner)
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
		if file, ok := c.In.(*os.File); ok && !terminalInput(file) {
			return c.failure(invalid("non_tty", ""))
		}
		c.print("welcome")
		if o.Spec.Namespace == "" || o.Spec.Name == "" {
			for {
				v, err := c.line("prompt_full_name")
				if err != nil {
					return c.failure(err)
				}
				if v == "\x1b" || v == "\x03" {
					c.print("cancelled")
					return 130
				}
				if !bunkopm.ValidName(v) {
					c.failure(invalid("invalid_name", v))
					continue
				}
				o.Spec.Namespace, o.Spec.Name, _ = strings.Cut(v, "/")
				break
			}
		}
		if !bunkopm.ValidName(o.Spec.FullName()) {
			return c.failure(invalid("invalid_name", o.Spec.FullName()))
		}
		child := o.Spec.Directory
		if child == "" {
			child = o.Spec.Name
		}
		if !validChild(child) {
			return c.failure(invalid("invalid_path", child))
		}
		if _, err := os.Lstat(filepath.Join(c.Root, child)); !os.IsNotExist(err) {
			return c.failure(invalid("exists", child))
		}
		if o.Spec.Description == "" {
			for {
				v, err := c.line("prompt_description")
				if err != nil {
					return c.failure(err)
				}
				if v == "\x1b" || v == "\x03" {
					c.print("cancelled")
					return 130
				}
				if !validDescription(v) {
					c.failure(invalid("invalid_description", ""))
					continue
				}
				o.Spec.Description = v
				break
			}
		}
		if len(o.Spec.Targets) == 0 {
			var err error
			o.Spec.Targets, err = c.choosePlatforms()
			if err != nil {
				return c.failure(err)
			}
		}
		if o.Advanced {
			if o.Spec.Version == "" {
				o.Spec.Version, e = c.line("prompt_version")
				if e != nil {
					return c.failure(e)
				}
			}
			if o.Spec.License == "" {
				o.Spec.License, e = c.line("prompt_license")
				if e != nil {
					return c.failure(e)
				}
			}
			if len(o.Spec.Dependencies) == 0 {
				v, err := c.line("prompt_dependencies")
				if err != nil {
					return c.failure(err)
				}
				parsed, err := Parse("create", []string{"--dependency", v})
				if err != nil {
					return c.failure(err)
				}
				o.Spec.Dependencies = parsed.Spec.Dependencies
			}
			if !o.Spec.Workflow {
				o.Spec.Workflow = c.yes("prompt_workflow")
			}
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
	if len(o.Spec.Dependencies) == 0 {
		c.print("summary", o.Spec.Version, o.Spec.License)
	} else {
		c.print("summary_dependencies", o.Spec.Version, o.Spec.License, strconv.Itoa(len(o.Spec.Dependencies)))
	}
	if o.Advanced && !o.NonInteractive && !c.yes("prompt_create") {
		c.print("cancelled")
		return 0
	}
	dir, e := Generate(c.Root, o.Spec)
	if e != nil {
		return c.failure(e)
	}
	c.print("created")
	c.print("location", o.Spec.FullName(), bunkopm.SafeText(dir))
	c.print("next")
	fmt.Fprintf(c.Out, "\n  cd %s\n  mio test .\n\n", o.Spec.Directory)
	c.print("publish_guide")
	if o.GitInit {
		if env.Git.Path == "" {
			return c.failure(invalid("missing_git", ""))
		}
		if c.permission("git_init", c.text("action_git_init", dir), o) {
			if e = gitInit(c.Runner, env.Git.Path, dir); e != nil {
				return c.failure(e)
			}
		}
	}
	if !o.Github && o.Advanced && !o.NonInteractive && env.Gh.Path != "" {
		o.Github = c.yes("prompt_github")
	}
	if o.Github {
		if e = c.connect(dir, o, env); e != nil && !isDeclined(e) {
			return c.failure(e)
		}
	}
	return 0
}
func (c *Context) failure(e error) int {
	var cancellation *Error
	if errors.As(e, &cancellation) && cancellation.Kind == "cancelled" {
		c.print("cancelled")
		return 130
	}
	kind, detail := "io", e.Error()
	var own *Error
	if errors.As(e, &own) {
		kind = own.Kind
		detail = own.Detail
	}
	text := "BUNKO_E_SCAFFOLD_" + strings.ToUpper(kind) + " " + c.text(kind, bunkopm.SafeText(detail))
	if c.Paint != nil {
		text = c.Paint("error", text)
	}
	fmt.Fprintln(c.Err, text)
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
