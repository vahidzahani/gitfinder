// GitFinder finds every local Git repository on the selected drives and
// reports branches, sync state and working tree status as an HTML page.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	appName      = "GitFinder"
	appVersion   = "1.0.0"
	authorName   = "vahidzahani"
	authorSite   = "vahidzahani.ir"
	authorGitHub = "github.com/vahidzahani"
)

type options struct {
	paths       string
	quick       bool
	fetch       bool
	noPrompt    bool
	noOpen      bool
	noEverythin bool
	out         string
	showVersion bool
}

func main() {
	var o options
	flag.StringVar(&o.paths, "path", "", "scan only these folders (separate several with ;)")
	flag.BoolVar(&o.quick, "quick", false, "re-check cached repositories only, without scanning disks")
	flag.BoolVar(&o.fetch, "fetch", false, "run git fetch on every repository before checking it")
	flag.BoolVar(&o.noPrompt, "yes", false, "skip the menu and scan the default drives")
	flag.BoolVar(&o.noOpen, "no-open", false, "do not open the report in the browser")
	flag.BoolVar(&o.noEverythin, "no-everything", false, "always walk the disks, even if Everything is running")
	flag.StringVar(&o.out, "out", "", "report file (default %LOCALAPPDATA%\\GitFinder\\report.html)")
	flag.BoolVar(&o.showVersion, "version", false, "print version and exit")
	flag.Parse()
	if o.showVersion {
		fmt.Println(appName, appVersion)
		return
	}

	initConsole()
	interactive := isTerminal() && !o.noPrompt && o.paths == "" && !o.quick
	code := run(o, interactive)
	if interactive {
		pause("Press Enter to exit…")
	}
	os.Exit(code)
}

func run(o options, interactive bool) int {
	gitVersion, err := checkGit()
	cache := loadCache()
	plan := scanPlan{fetch: o.fetch}
	if !o.noEverythin && everythingReady() {
		on := true
		plan.everything = &on
	}
	header := func() {
		printBanner()
		if err != nil {
			return
		}
		okLine("Git found: " + gitVersion)
		if plan.everything != nil {
			okLine("Everything is running: instant scan available")
		}
		if n := len(cache.Repos); n > 0 {
			okLine(fmt.Sprintf("Cache: %d repositories (last scan %s)", n, ago(cache.LastScan)))
		} else {
			infoLine("Cache: empty, this is the first scan")
		}
		fmt.Println()
	}
	header()
	if err != nil {
		printGitMissing()
		return 1
	}

	switch {
	case o.quick:
		if len(cache.Repos) == 0 {
			errLine("The cache is empty; run a full scan first.")
			return 2
		}
		plan.mode = modeQuick
	case o.paths != "":
		plan.mode = modePath
		for _, p := range strings.Split(o.paths, ";") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			p = cleanPath(p)
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				errLine("Not a folder: " + p)
				return 2
			}
			plan.roots = append(plan.roots, p)
		}
	default:
		drives := listDrives()
		applySelection(drives, cache.SelectedDrives)
		if interactive {
			var ok bool
			if plan, ok = runMenu(drives, plan, len(cache.Repos), header); !ok {
				return 0
			}
		} else {
			plan.mode, plan.roots = modeDrives, selectedRoots(drives)
		}
		if plan.mode == modeDrives {
			cache.SelectedDrives = plan.roots
		}
	}
	if plan.mode != modeQuick && len(plan.roots) == 0 {
		errLine("Nothing to scan.")
		return 2
	}

	start := time.Now()
	var paths []string
	var folders int64
	if plan.mode == modeQuick {
		paths = cache.knownPaths()
		okLine(fmt.Sprintf("Quick refresh of %s from the cache", bold(fmt.Sprintf("%d repositories", len(paths)))))
	} else {
		paths, folders = findRepos(plan, cache)
	}

	repos := inspectAll(paths, cache.get, plan.fetch)
	sort.Slice(repos, func(i, j int) bool { return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name) })
	markDuplicates(repos)
	cache.update(plan.roots, repos, plan.mode != modeQuick)
	if err := cache.save(); err != nil {
		warnLine("Could not save the cache: " + err.Error())
	}
	took := time.Since(start)
	printSummary(repos, took)

	out := o.out
	if out == "" {
		out = filepath.Join(appDataDir(), "report.html")
	}
	out, _ = filepath.Abs(out)
	err = writeReport(out, reportData{
		Mode:    [...]string{"drives", "path", "quick"}[plan.mode],
		Roots:   plan.roots,
		Fetched: plan.fetch,
		Seconds: took.Seconds(),
		Folders: folders,
		Repos:   repos,
	})
	if err != nil {
		errLine("Could not write the report: " + err.Error())
		return 1
	}
	okLine("Report: " + green(out))
	if !o.noOpen {
		if err := openInBrowser(out); err != nil {
			warnLine("Could not open the browser: " + err.Error())
		}
	}
	return 0
}

func printSummary(repos []*Repo, took time.Duration) {
	var dirty, unpushed, noRemote, behind, failed int
	var risky []*Repo
	for _, r := range repos {
		if r.Error != "" {
			failed++
			continue
		}
		if !r.Clean() {
			dirty++
		}
		if len(r.Remotes) == 0 {
			noRemote++
		} else if r.Unpushed > 0 {
			unpushed++
		}
		if r.Behind > 0 {
			behind++
		}
		if len(r.Remotes) == 0 && (r.Commits > 0 || !r.Clean()) || len(r.Remotes) > 0 && r.Unpushed > 0 {
			risky = append(risky, r)
		}
	}
	okLine(fmt.Sprintf("Checked %d repositories in %s", len(repos), took.Round(100*time.Millisecond)))
	fmt.Println()
	fmt.Printf("    %s  %s  %s  %s  %s\n",
		bold(fmt.Sprintf("%d repos", len(repos))),
		yellow(fmt.Sprintf("%d dirty", dirty)),
		yellow(fmt.Sprintf("%d unpushed", unpushed)),
		red(fmt.Sprintf("%d no remote", noRemote)),
		cyan(fmt.Sprintf("%d behind", behind)))
	if failed > 0 {
		warnLine(fmt.Sprintf("%d repositories could not be read", failed))
	}
	if len(risky) > 0 {
		fmt.Println()
		fmt.Println("  " + red("⚠") + " " + bold(fmt.Sprintf("At risk: %d repositories have work that exists only on this computer", len(risky))))
		for i, r := range risky {
			if i == 8 {
				fmt.Println("      " + dim(fmt.Sprintf("… and %d more (see the report)", len(risky)-8)))
				break
			}
			why := "no remote"
			if len(r.Remotes) > 0 {
				why = fmt.Sprintf("%d unpushed commits", r.Unpushed)
			}
			fmt.Printf("      %s %s  %s\n", padRight(bold(r.Name), 24), padRight(red(why), 20), dim(r.Path))
		}
	}
	fmt.Println()
}

func checkGit() (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", err
	}
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "git version "), nil
}

func printGitMissing() {
	errLine(bold("Git is not installed") + " (or it is not on PATH).")
	fmt.Println()
	fmt.Println("    GitFinder needs Git to read repository status. Install it, then run GitFinder again:")
	fmt.Println()
	fmt.Println("      " + green("winget install --id Git.Git -e --source winget"))
	fmt.Println("      " + dim("or download it from ") + cyan("https://git-scm.com/download/win"))
	fmt.Println()
	fmt.Println("    " + dim("After installing, open a new terminal so PATH is refreshed."))
}

// findRepos discovers repositories under the plan's roots, through the
// Everything index when enabled and by walking the disks otherwise.
func findRepos(plan scanPlan, cache *Cache) ([]string, int64) {
	walk := plan.roots
	var paths []string
	if plan.everything != nil && *plan.everything {
		walk = nil
		var indexed []string
		for _, r := range plan.roots {
			if everythingIndexed(r) {
				indexed = append(indexed, r)
			} else {
				walk = append(walk, r)
			}
		}
		if len(indexed) > 0 {
			start := time.Now()
			found, err := everythingRepos(indexed)
			if err != nil {
				warnLine("Everything query failed (" + err.Error() + "), scanning the disks instead")
				walk = plan.roots
			} else {
				// Everything's index can lag behind; keep repos found by
				// earlier scans that still exist.
				known := 0
				for _, p := range cache.knownPaths() {
					if underAny(repoKey(p), indexed) && !contains(found, p) && !skippedPath(p) {
						found = append(found, p)
						known++
					}
				}
				paths = dropNested(found)
				msg := fmt.Sprintf("Everything index: %s on %s in %s", bold(fmt.Sprintf("%d repositories", len(paths))),
					strings.Join(indexed, ", "), time.Since(start).Round(time.Millisecond))
				if known > 0 {
					msg += dim(fmt.Sprintf(" (+%d from cache)", known))
				}
				okLine(msg)
			}
		}
	}
	var folders int64
	if len(walk) > 0 {
		found, st := discover(walk)
		folders = st.dirs.Load()
		okLine(fmt.Sprintf("Found %s in %s folders", bold(fmt.Sprintf("%d repositories", len(found))), thousands(folders)))
		paths = append(paths, found...)
	}
	return paths, folders
}
