package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Folders that never contain the user's own repositories but are huge.
var skipAnywhere = map[string]bool{
	"node_modules": true, "bower_components": true, "__pycache__": true,
	"site-packages": true, ".venv": true, ".tox": true, ".vs": true,
	".npm": true, ".nuget": true, ".gradle": true, ".m2": true,
	".cargo": true, ".rustup": true, ".pnpm-store": true,
	"appdata": true, "windowsapps": true, "vendor": true,
	"$recycle.bin": true, "system volume information": true,
}

// Folders skipped only directly under a drive root (C:\Windows etc.).
var skipAtRoot = map[string]bool{
	"windows": true, "program files": true, "program files (x86)": true,
	"programdata": true, "recovery": true, "perflogs": true,
	"msocache": true, "config.msi": true, "intel": true,
}

type scanStats struct {
	dirs    atomic.Int64
	repos   atomic.Int64
	errors  atomic.Int64
	current atomic.Pointer[string]
}

func shouldSkip(parent, name string, atRoot bool) bool {
	l := strings.ToLower(name)
	if skipAnywhere[l] {
		return true
	}
	if atRoot && (skipAtRoot[l] || strings.HasPrefix(l, "$")) {
		return true
	}
	// Go module cache: ~/go/pkg/mod
	return l == "mod" && strings.EqualFold(filepath.Base(parent), "pkg")
}

func isDriveRoot(dir string) bool {
	return len(dir) == 3 && dir[1] == ':' && (dir[2] == '\\' || dir[2] == '/')
}

// walkRoots walks all roots in parallel and returns every folder that holds
// a .git directory (or a .git file, for worktrees). It does not descend into
// repositories it found.
func walkRoots(roots []string, st *scanStats) []string {
	var (
		mu     sync.Mutex
		cond   = sync.NewCond(&mu)
		stack  = append([]string(nil), roots...)
		active int
		found  []string
		wg     sync.WaitGroup
	)
	workers := min(32, max(8, runtime.NumCPU()*2))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				for len(stack) == 0 && active > 0 {
					cond.Wait()
				}
				if len(stack) == 0 {
					mu.Unlock()
					cond.Broadcast()
					return
				}
				dir := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				active++
				mu.Unlock()

				isRepo, subdirs := visit(dir, st)

				mu.Lock()
				if isRepo {
					found = append(found, dir)
				}
				stack = append(stack, subdirs...)
				active--
				if len(subdirs) > 0 || active == 0 {
					cond.Broadcast()
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return found
}

func visit(dir string, st *scanStats) (bool, []string) {
	st.dirs.Add(1)
	st.current.Store(&dir)

	f, err := os.Open(dir)
	if err != nil {
		st.errors.Add(1)
		return false, nil
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil && len(entries) == 0 {
		st.errors.Add(1)
		return false, nil
	}

	for _, e := range entries {
		if strings.EqualFold(e.Name(), ".git") && isGitMarker(dir, e) {
			st.repos.Add(1)
			return true, nil
		}
	}

	atRoot := isDriveRoot(dir)
	var subdirs []string
	for _, e := range entries {
		// Skip symlinks and junctions to avoid loops and double counting.
		if !e.IsDir() || e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		if shouldSkip(dir, e.Name(), atRoot) {
			continue
		}
		subdirs = append(subdirs, filepath.Join(dir, e.Name()))
	}
	return false, subdirs
}

// isGitMarker accepts a real .git folder (has HEAD) or a .git file that
// points to a git dir (worktrees and submodules).
func isGitMarker(dir string, e fs.DirEntry) bool {
	p := filepath.Join(dir, e.Name())
	if e.IsDir() {
		_, err := os.Stat(filepath.Join(p, "HEAD"))
		return err == nil
	}
	b := make([]byte, 8)
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	n, _ := f.Read(b)
	return n == 8 && string(b) == "gitdir: "
}

// discover walks the roots while drawing a live progress line.
func discover(roots []string) ([]string, *scanStats) {
	st := &scanStats{}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		spin := `|/-\`
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(120 * time.Millisecond):
			}
			cur := ""
			if p := st.current.Load(); p != nil {
				cur = *p
			}
			statusLine(fmt.Sprintf("  %s Scanning  %s folders  %s  ",
				green(string(spin[i%4])), thousands(st.dirs.Load()),
				bgreen(fmt.Sprintf("%d repos", st.repos.Load()))), cur)
		}
	}()
	found := walkRoots(roots, st)
	close(stop)
	<-stopped
	clearStatus()
	return found, st
}
