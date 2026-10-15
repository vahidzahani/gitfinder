package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type scanMode int

const (
	modeDrives scanMode = iota
	modePath
)

type scanPlan struct {
	mode  scanMode
	roots []string
	fetch bool
}

// runMenu shows the interactive drive picker until the user starts a scan
// (true) or exits (false). header redraws the banner and environment info.
func runMenu(drives []Drive, plan scanPlan, header func()) (scanPlan, bool) {
	msg := ""
	for {
		clearScreen()
		header()
		printMenu(drives, plan)
		if msg != "" {
			fmt.Println("  " + msg)
			fmt.Println()
			msg = ""
		}
		in, ok := readLine("  " + bgreen("›") + " ")
		if !ok {
			return plan, false
		}
		lower := strings.ToLower(in)
		switch {
		case in == "":
			roots := selectedRoots(drives)
			if len(roots) == 0 {
				msg = red("Select at least one drive, or type a folder path.")
				continue
			}
			plan.mode, plan.roots = modeDrives, roots
			return plan, true
		case lower == "x":
			return plan, false
		case lower == "f":
			plan.fetch = !plan.fetch
		case lower == "a" || lower == "n":
			for i := range drives {
				drives[i].Selected = lower == "a" && drives[i].Ready
			}
		case isNumberList(in):
			for _, f := range splitList(in) {
				n, _ := strconv.Atoi(f)
				switch {
				case n < 1 || n > len(drives):
					msg = red(fmt.Sprintf("There is no drive number %d.", n))
				case !drives[n-1].Ready:
					msg = yellow(drives[n-1].Root + " is not ready.")
				default:
					drives[n-1].Selected = !drives[n-1].Selected
				}
			}
		default:
			p := cleanPath(in)
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				plan.mode, plan.roots = modePath, []string{p}
				return plan, true
			}
			msg = red("Not a folder: ") + p
		}
	}
}

func printMenu(drives []Drive, plan scanPlan) {
	fmt.Println("  " + bold("Drives") + dim("  (type a number to toggle, e.g. 2 or 1 3)"))
	for i, d := range drives {
		mark := check(d.Selected)
		size := dim("not ready")
		if !d.Ready {
			mark = dim("[-]")
		} else {
			size = fmt.Sprintf("%s free of %s", humanBytes(d.Free), humanBytes(d.Total))
		}
		label := d.Label
		if label == "" {
			label = dim("—")
		}
		fmt.Printf("   %s %2d  %s  %s %s %s\n", mark, i+1, bold(d.Root),
			padRight(shortenLeft(label, 16), 16), padRight(d.Kind, 10), size)
	}
	fmt.Println()
	fmt.Println("  " + bold("Options"))
	fmt.Println("   " + check(plan.fetch) + " " + green("F") + "  Fetch from remotes first  " +
		dim("(exact sync state; slower, needs network)"))
	fmt.Println()
	fmt.Println("  " + bold("Commands"))
	fmt.Println("   " + green("A") + " / " + green("N") + "  select all / none drives      " + green("X") + "  exit")
	fmt.Println()
	fmt.Println("  " + bgreen("Enter") + " = scan selected drives   " + dim("·") + "   or type/paste a folder path to scan only that folder")
	fmt.Println()
}

func check(on bool) string {
	if on {
		return bgreen("[x]")
	}
	return dim("[ ]")
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' })
}

func isNumberList(s string) bool {
	fields := splitList(s)
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if _, err := strconv.Atoi(f); err != nil {
			return false
		}
	}
	return true
}

// cleanPath normalizes user input such as "D:", "d:\projects\" or a quoted path.
func cleanPath(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	if len(s) == 2 && s[1] == ':' {
		s += `\`
	}
	if abs, err := filepath.Abs(s); err == nil {
		s = abs
	}
	return s
}
