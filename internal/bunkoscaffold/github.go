package bunkoscaffold

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"Mio/internal/bunkopm"
)

var errNeedLogin = errors.New("GitHub login required")

type account struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}
type authEntry struct {
	Login, State string
	Active       bool
}
type repositoryInfo struct {
	FullName    string `json:"full_name"`
	Private     bool
	Owner       struct{ Login, Type string }
	Permissions struct{ Push, Admin bool }
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

func validLogin(s string) bool { return bunkopm.ValidName(strings.ToLower(s) + "/package") }
func parseRepo(s string) (string, string, error) {
	owner, name, ok := strings.Cut(s, "/")
	owner = strings.ToLower(owner)
	if !ok || !validLogin(owner) || !repoName.MatchString(name) || name == "." || name == ".." {
		return "", "", invalid("namespace_auth", "")
	}
	return owner, name, nil
}
func ghAPI(r Runner, path, endpoint, filter string, input []byte) ([]byte, error) {
	method := "GET"
	if input != nil {
		method = "POST"
	}
	args := []string{"api", "--hostname", "github.com", "--method", method, endpoint, "--jq", filter}
	if input != nil {
		args = append(args, "--input", "-")
	}
	return r.Run(Command{Path: path, Args: args, Input: input, Timeout: 20 * time.Second})
}
func activeAccount(r Runner, gh string) (account, int, error) {
	// The jq filter runs INSIDE gh. Token/scopes fields never enter our process.
	// --no-refresh is mandatory; older gh versions fail closed, without retrying.
	b, e := r.Run(Command{Path: gh, Args: []string{"auth", "status", "--hostname", "github.com", "--no-refresh", "--json", "hosts", "--jq", `(.hosts["github.com"] // []) | map({login, active, state})`}, Timeout: 15 * time.Second})
	if e != nil {
		return account{}, 0, invalid("auth", "")
	}
	var entries []authEntry
	if json.Unmarshal(b, &entries) != nil {
		return account{}, 0, invalid("auth", "")
	}
	selected := ""
	for _, a := range entries {
		if a.Active && a.State == "timeout" {
			return account{}, len(entries), invalid("auth", "")
		}
		if a.Active && a.State == "success" && validLogin(a.Login) {
			if selected != "" {
				return account{}, len(entries), invalid("auth", "")
			}
			selected = a.Login
		}
	}
	if selected == "" {
		return account{}, len(entries), errNeedLogin
	}
	b, e = ghAPI(r, gh, "user", "{login,id}", nil)
	if e != nil {
		return account{}, len(entries), invalid("auth", "")
	}
	var a account
	if json.Unmarshal(b, &a) != nil || !validLogin(a.Login) || a.ID <= 0 || !strings.EqualFold(a.Login, selected) {
		return account{}, len(entries), invalid("auth", "")
	}
	return a, len(entries), nil
}
func sameAccount(r Runner, gh string, expected account) error {
	a, _, e := activeAccount(r, gh)
	if e != nil || a.ID != expected.ID || !strings.EqualFold(a.Login, expected.Login) {
		return invalid("auth", "")
	}
	return nil
}
func namespaceOwner(namespace, owner string, a account) error {
	switch namespace {
	case "bunko":
		return invalid("namespace_auth", namespace)
	case "mio", "kiso":
		if owner != "mio-kobo" || a.ID != 72144156 {
			return invalid("namespace_auth", namespace)
		}
	default:
		if namespace != owner {
			return invalid("namespace_auth", namespace)
		}
	}
	return nil
}
func gitInit(r Runner, git, dir string) error {
	_, e := r.Run(Command{Path: git, Git: true, Args: []string{"init"}, Dir: dir, Timeout: 10 * time.Second})
	if e != nil {
		return invalid("process", "git init")
	}
	return nil
}
func gitState(r Runner, git, dir string) (bool, error) {
	p := filepath.Join(dir, ".git")
	s, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil || !s.IsDir() || s.Mode()&os.ModeSymlink != 0 {
		return false, invalid("git_config", "")
	}
	if s, e = os.Lstat(filepath.Join(p, "config")); e != nil || !s.Mode().IsRegular() {
		return false, invalid("git_config", "")
	}
	b, e := r.Run(Command{Path: git, Git: true, Args: []string{"rev-parse", "--show-toplevel"}, Dir: dir, Timeout: 5 * time.Second})
	if e != nil {
		return false, invalid("git_config", "")
	}
	actual, e := filepath.EvalSymlinks(strings.TrimSpace(string(b)))
	if e != nil || !samePath(actual, dir) {
		return false, invalid("git_config", "")
	}
	// Only remote NAMES are read. URLs could contain credentials and are never read.
	b, e = r.Run(Command{Path: git, Git: true, Args: []string{"remote"}, Dir: dir, Timeout: 5 * time.Second})
	if e != nil {
		return false, invalid("git_config", "")
	}
	for _, name := range strings.Fields(string(b)) {
		if name == "origin" {
			return false, invalid("git_config", "")
		}
	}
	return true, nil
}
func (c *Context) connect(root string, o Options, env Environment) error {
	dir, e := filepath.EvalSymlinks(root)
	if e != nil {
		return invalid("not_package", root)
	}
	dir, e = filepath.Abs(dir)
	if e != nil {
		return e
	}
	info, e := ReadInfo(dir)
	if e != nil {
		return e
	}
	original, e := os.ReadFile(filepath.Join(dir, InfoName))
	if e != nil {
		return e
	}
	if env.Gh.Path == "" {
		return invalid("missing_gh", "")
	}
	if env.Git.Path == "" {
		return invalid("missing_git", "")
	}
	hasGit, e := gitState(c.Runner, env.Git.Path, dir)
	if e != nil {
		return e
	}
	a, count, e := activeAccount(c.Runner, env.Gh.Path)
	if e != nil {
		if !errors.Is(e, errNeedLogin) {
			return e
		}
		if !c.permission("login", c.text("action_login"), o) {
			return errDeclined
		}
		if e = c.Runner.Interactive(Command{Path: env.Gh.Path, Args: []string{"auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https"}, Timeout: 180 * time.Second}, c.reader, c.Out, c.Err); e != nil {
			return invalid("auth", "")
		}
		a, count, e = activeAccount(c.Runner, env.Gh.Path)
		if e != nil {
			return invalid("auth", "")
		}
	}
	c.print("active_account", a.Login)
	if count > 1 {
		c.print("multiple_accounts")
		if o.NonInteractive && o.AllowedAccount != a.Login {
			return errDeclined
		}
		if !c.permission("account", c.text("active_account", a.Login), o) {
			return errDeclined
		}
	}
	if o.Repo == "" && !o.NonInteractive {
		o.Repo, e = c.line("prompt_repo")
		if e != nil {
			return e
		}
	}
	owner, name, e := parseRepo(o.Repo)
	if e != nil {
		return e
	}
	ns, _, _ := strings.Cut(info.Package.Name, "/")
	if e = namespaceOwner(ns, owner, a); e != nil {
		return e
	}
	full := owner + "/" + name
	if o.NewRepo {
		if o.Visibility == "" && !o.NonInteractive {
			o.Visibility, e = c.line("prompt_visibility")
			if e != nil {
				return e
			}
		}
		if o.Visibility != "public" && o.Visibility != "private" {
			return invalid("options", "")
		}
		if !strings.EqualFold(owner, a.Login) {
			b, e := ghAPI(c.Runner, env.Gh.Path, "user/memberships/orgs/"+owner, "{state,role}", nil)
			var membership struct{ State, Role string }
			if e != nil || json.Unmarshal(b, &membership) != nil || membership.State != "active" || membership.Role != "admin" {
				return invalid("namespace_auth", owner)
			}
		}
		preview := c.text("remote_preview", a.Login, full, o.Visibility, c.text("action_repo_create"))
		if !c.permission("repo_create", preview, o) {
			return errDeclined
		}
		if e = sameAccount(c.Runner, env.Gh.Path, a); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"name": name, "description": info.Package.Description, "private": o.Visibility == "private", "auto_init": false})
		endpoint := "user/repos"
		if !strings.EqualFold(owner, a.Login) {
			endpoint = "orgs/" + owner + "/repos"
		}
		if _, e = ghAPI(c.Runner, env.Gh.Path, endpoint, "{full_name}", payload); e != nil {
			return invalid("repository_create", full)
		}
		c.print("repository_created", full)
	}
	b, e := ghAPI(c.Runner, env.Gh.Path, "repos/"+full, "{full_name,private,owner:{login:.owner.login,type:.owner.type},permissions:{push:.permissions.push,admin:.permissions.admin}}", nil)
	var repo repositoryInfo
	if e != nil || json.Unmarshal(b, &repo) != nil || !strings.EqualFold(repo.FullName, full) || !strings.EqualFold(repo.Owner.Login, owner) || (!repo.Permissions.Push && !repo.Permissions.Admin) {
		return invalid("repository_permission", full)
	}
	visibility := "public"
	if repo.Private {
		visibility = "private"
	}
	if !c.permission("connect", c.text("remote_preview", a.Login, full, visibility, c.text("action_connect")), o) {
		return errDeclined
	}
	if !hasGit {
		if !c.permission("git_init", c.text("action_git_init", dir), o) {
			return errDeclined
		}
		if e = gitInit(c.Runner, env.Git.Path, dir); e != nil {
			return e
		}
	}
	url := "https://github.com/" + full
	if !c.permission("git_config", c.text("action_git_config", dir, url), o) {
		return errDeclined
	}
	if e = sameAccount(c.Runner, env.Gh.Path, a); e != nil {
		return e
	}
	if _, e = c.Runner.Run(Command{Path: env.Git.Path, Git: true, Args: []string{"remote", "add", "origin", url}, Dir: dir, Timeout: 10 * time.Second}); e != nil {
		return invalid("git_config", "")
	}
	// Detect edits made while the user was considering permissions. Do not replace them.
	current, e := os.ReadFile(filepath.Join(dir, InfoName))
	if e != nil || !bytes.Equal(original, current) {
		return invalid("io", InfoName)
	}
	info.Package.Repository = url
	if e = saveInfo(dir, info); e != nil {
		return invalid("io", InfoName)
	}
	c.print("connected", full)
	return nil
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
