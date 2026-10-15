package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Branch struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Gone     bool   `json:"gone,omitempty"`
	Current  bool   `json:"current,omitempty"`
	Date     int64  `json:"date"`
}

type Remote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Key  string `json:"key"`           // normalized URL used to find duplicate clones
	Web  string `json:"web,omitempty"` // browser link, when one can be derived
}

type Repo struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Worktree    bool     `json:"worktree,omitempty"`
	Branch      string   `json:"branch"`
	Detached    bool     `json:"detached,omitempty"`
	HeadOID     string   `json:"head"`
	Upstream    string   `json:"upstream,omitempty"`
	Ahead       int      `json:"ahead"`
	Behind      int      `json:"behind"`
	Staged      int      `json:"staged"`
	Modified    int      `json:"modified"`
	Untracked   int      `json:"untracked"`
	Conflicts   int      `json:"conflicts"`
	Branches    []Branch `json:"branches"`
	Remotes     []Remote `json:"remotes"`
	Commits     int      `json:"commits"`
	LastCommit  int64    `json:"lastCommit"`
	LastAuthor  string   `json:"lastAuthor,omitempty"`
	LastSubject string   `json:"lastSubject,omitempty"`
	Stashes     int      `json:"stashes"`
	Unpushed    int      `json:"unpushed"` // commits on local branches that no remote has
	Fetched     bool     `json:"fetched,omitempty"`
	FetchError  string   `json:"fetchError,omitempty"`
	Error       string   `json:"error,omitempty"`
	CheckedAt   int64    `json:"checkedAt"`
	Duplicates  []string `json:"duplicates,omitempty"`
}

func (r *Repo) Clean() bool {
	return r.Staged+r.Modified+r.Untracked+r.Conflicts == 0
}

var gitEnv = append(os.Environ(),
	"GIT_TERMINAL_PROMPT=0", // never ask for credentials
	"GCM_INTERACTIVE=never",
	"GIT_OPTIONAL_LOCKS=0", // status must not touch the index lock
	"LC_ALL=C", "LANGUAGE=C",
)

func init() {
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" {
		gitEnv = append(gitEnv, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=15")
	}
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	base := []string{"-C", dir, "-c", "safe.directory=*", "-c", "core.quotepath=off",
		"-c", "color.ui=never", "--no-pager"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = gitEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			msg = "timed out"
		} else if msg == "" {
			msg = err.Error()
		}
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		return out.String(), errors.New(strings.TrimPrefix(msg, "fatal: "))
	}
	return out.String(), nil
}

// inspect collects everything the report needs for one repository. prev is
// the cached result from the last run; commit stats are reused from it when
// HEAD has not moved.
func inspect(path string, prev *Repo, fetch bool) *Repo {
	r := &Repo{Path: path, Name: filepath.Base(path), CheckedAt: time.Now().Unix()}
	if fi, err := os.Lstat(filepath.Join(path, ".git")); err == nil && !fi.IsDir() {
		r.Worktree = true
	}

	if fetch {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		_, err := git(ctx, path, "fetch", "--all", "--prune", "--quiet")
		cancel()
		r.Fetched = err == nil
		if err != nil {
			r.FetchError = err.Error()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	out, err := git(ctx, path, "status", "--porcelain=v2", "--branch")
	if err != nil {
		r.Error = err.Error()
		return r
	}
	parseStatus(r, out)

	if out, err := git(ctx, path, "for-each-ref",
		"--format=%(HEAD)%09%(refname:short)%09%(upstream:short)%09%(upstream:track,nobracket)%09%(committerdate:unix)",
		"refs/heads"); err == nil {
		r.Branches = parseBranches(out)
	}
	if out, err := git(ctx, path, "config", "--get-regexp", `^remote\..*\.url$`); err == nil {
		r.Remotes = parseRemotes(out)
	}

	if r.HeadOID != "" {
		if prev != nil && prev.HeadOID == r.HeadOID && prev.Commits > 0 {
			r.Commits, r.LastCommit = prev.Commits, prev.LastCommit
			r.LastAuthor, r.LastSubject = prev.LastAuthor, prev.LastSubject
		} else {
			if out, err := git(ctx, path, "rev-list", "--count", "HEAD"); err == nil {
				r.Commits, _ = strconv.Atoi(strings.TrimSpace(out))
			}
			if out, err := git(ctx, path, "log", "-1", "--format=%ct%x09%an%x09%s"); err == nil {
				if f := strings.SplitN(strings.TrimSpace(out), "\t", 3); len(f) == 3 {
					r.LastCommit, _ = strconv.ParseInt(f[0], 10, 64)
					r.LastAuthor, r.LastSubject = f[1], f[2]
				}
			}
		}
	}

	if len(r.Branches) > 0 {
		args := []string{"rev-list", "--count", "--branches"}
		if len(r.Remotes) > 0 {
			args = append(args, "--not", "--remotes")
		}
		if out, err := git(ctx, path, args...); err == nil {
			r.Unpushed, _ = strconv.Atoi(strings.TrimSpace(out))
		}
	}
	if out, err := git(ctx, path, "rev-list", "--walk-reflogs", "--count", "refs/stash"); err == nil {
		r.Stashes, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	return r
}

func parseStatus(r *Repo, out string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			if v := line[len("# branch.oid "):]; v != "(initial)" {
				r.HeadOID = v
			}
		case strings.HasPrefix(line, "# branch.head "):
			if v := line[len("# branch.head "):]; v == "(detached)" {
				r.Detached = true
			} else {
				r.Branch = v
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			r.Upstream = line[len("# branch.upstream "):]
		case strings.HasPrefix(line, "# branch.ab "):
			fmt.Sscanf(line[len("# branch.ab "):], "+%d -%d", &r.Ahead, &r.Behind)
		case (line[0] == '1' || line[0] == '2') && len(line) > 3:
			if line[2] != '.' {
				r.Staged++
			}
			if line[3] != '.' {
				r.Modified++
			}
		case line[0] == 'u':
			r.Conflicts++
		case line[0] == '?':
			r.Untracked++
		}
	}
}

func parseBranches(out string) []Branch {
	var list []Branch
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) != 5 {
			continue
		}
		b := Branch{Current: f[0] == "*", Name: f[1], Upstream: f[2]}
		b.Date, _ = strconv.ParseInt(f[4], 10, 64)
		for _, part := range strings.Split(f[3], ",") {
			part = strings.TrimSpace(part)
			switch {
			case part == "gone":
				b.Gone = true
			case strings.HasPrefix(part, "ahead "):
				b.Ahead, _ = strconv.Atoi(part[6:])
			case strings.HasPrefix(part, "behind "):
				b.Behind, _ = strconv.Atoi(part[7:])
			}
		}
		list = append(list, b)
	}
	return list
}

func parseRemotes(out string) []Remote {
	var list []Remote
	for _, line := range strings.Split(out, "\n") {
		key, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		norm, web := normalizeRemote(url)
		list = append(list, Remote{Name: name, URL: url, Key: norm, Web: web})
	}
	return list
}

// normalizeRemote turns https://github.com/A/b.git, git@github.com:A/b and
// ssh://git@github.com:22/A/b into the same key "github.com/a/b".
func normalizeRemote(u string) (key, web string) {
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(u), "/"), ".git")
	if len(s) >= 2 && s[1] == ':' || strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, "file://") {
		return strings.ToLower(filepath.Clean(strings.TrimPrefix(s, "file://"))), ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		host, rest, _ := strings.Cut(s, "/")
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		if c := strings.Index(host, ":"); c >= 0 {
			host = host[:c]
		}
		s = host + "/" + rest
	} else if at, c := strings.Index(s, "@"), strings.Index(s, ":"); c > at {
		s = s[at+1:c] + "/" + strings.TrimPrefix(s[c+1:], "/")
	}
	if host, _, _ := strings.Cut(s, "/"); strings.Contains(host, ".") {
		web = "https://" + s
	}
	return strings.ToLower(s), web
}

// inspectAll checks repositories in parallel and draws a progress line.
func inspectAll(paths []string, prev func(string) *Repo, fetch bool) []*Repo {
	repos := make([]*Repo, len(paths))
	workers := min(16, max(4, runtime.NumCPU()))
	if fetch {
		workers = 8
	}
	var done atomic.Int64
	var last atomic.Pointer[string]
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				last.Store(&paths[i])
				repos[i] = inspect(paths[i], prev(paths[i]), fetch)
				done.Add(1)
			}
		}()
	}

	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		verb := "Checking"
		if fetch {
			verb = "Fetching & checking"
		}
		for {
			cur := ""
			if p := last.Load(); p != nil {
				cur = *p
			}
			statusLine(fmt.Sprintf("  %s %s repositories  %s  ", green("»"), verb,
				bgreen(fmt.Sprintf("%d/%d", done.Load(), len(paths)))), cur)
			select {
			case <-stop:
				return
			case <-time.After(120 * time.Millisecond):
			}
		}
	}()

	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(stop)
	<-stopped
	clearStatus()
	return repos
}
