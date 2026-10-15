// GitFinder finds every local Git repository on the selected drives and
// reports branches, sync state and working tree status as an HTML page.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
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
	noPrompt    bool
	showVersion bool
}

func main() {
	var o options
	flag.StringVar(&o.paths, "path", "", "scan only these folders (separate several with ;)")
	flag.BoolVar(&o.noPrompt, "yes", false, "skip the menu and scan the default drives")
	flag.BoolVar(&o.showVersion, "version", false, "print version and exit")
	flag.Parse()
	if o.showVersion {
		fmt.Println(appName, appVersion)
		return
	}

	initConsole()
	interactive := isTerminal() && !o.noPrompt && o.paths == ""
	code := run(o, interactive)
	if interactive {
		pause("Press Enter to exit…")
	}
	os.Exit(code)
}

func run(o options, interactive bool) int {
	gitVersion, err := checkGit()
	header := func() {
		printBanner()
		if err == nil {
			okLine("Git found: " + gitVersion)
		}
		fmt.Println()
	}
	header()
	if err != nil {
		printGitMissing()
		return 1
	}

	plan := scanPlan{}
	if o.paths != "" {
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
	} else {
		drives := listDrives()
		if interactive {
			var ok bool
			if plan, ok = runMenu(drives, plan, header); !ok {
				return 0
			}
		} else {
			plan.mode, plan.roots = modeDrives, selectedRoots(drives)
		}
	}
	if len(plan.roots) == 0 {
		errLine("Nothing to scan.")
		return 2
	}

	start := time.Now()
	paths, st := discover(plan.roots)
	okLine(fmt.Sprintf("Found %s in %s folders (%s)", bold(fmt.Sprintf("%d repositories", len(paths))),
		thousands(st.dirs.Load()), time.Since(start).Round(100*time.Millisecond)))
	for _, p := range paths {
		infoLine(p)
	}
	return 0
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
