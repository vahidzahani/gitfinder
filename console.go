package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procSetConsoleOutputCP         = kernel32.NewProc("SetConsoleOutputCP")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetErrorMode               = kernel32.NewProc("SetErrorMode")
)

var (
	useColor bool
	stdin    = bufio.NewReader(os.Stdin)
)

// initConsole switches the console to UTF-8, enables ANSI colors and
// suppresses "insert disk" dialogs for empty card readers / DVD drives.
func initConsole() {
	const semFailCriticalErrors, semNoOpenFileErrorBox = 0x0001, 0x8000
	procSetErrorMode.Call(semFailCriticalErrors | semNoOpenFileErrorBox)
	procSetConsoleOutputCP.Call(65001)

	const enableVirtualTerminalProcessing = 0x0004
	var mode uint32
	h := os.Stdout.Fd()
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
		r, _, _ = procSetConsoleMode.Call(h, uintptr(mode|enableVirtualTerminalProcessing))
		useColor = r != 0
	}
	if os.Getenv("NO_COLOR") != "" {
		useColor = false
	}
}

// isTerminal reports whether stdin is an interactive console.
func isTerminal() bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(os.Stdin.Fd(), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

type consoleScreenBufferInfo struct {
	size, cursor       [2]int16
	attributes         uint16
	left, top          int16
	right, bottom      int16
	maxSizeX, maxSizeY int16
}

func consoleWidth() int {
	var info consoleScreenBufferInfo
	if r, _, _ := procGetConsoleScreenBufferInfo.Call(os.Stdout.Fd(), uintptr(unsafe.Pointer(&info))); r != 0 {
		if w := int(info.right-info.left) + 1; w > 20 {
			return w
		}
	}
	return 100
}

func clearScreen() {
	if useColor {
		fmt.Print("\x1b[H\x1b[2J\x1b[3J")
	}
}

func paint(code, s string) string {
	if !useColor || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string   { return paint("1", s) }
func dim(s string) string    { return paint("2", s) }
func green(s string) string  { return paint("32", s) }
func bgreen(s string) string { return paint("1;32", s) }
func red(s string) string    { return paint("31", s) }
func yellow(s string) string { return paint("33", s) }
func cyan(s string) string   { return paint("36", s) }

func okLine(s string)   { fmt.Println("  " + green("✔") + " " + s) }
func warnLine(s string) { fmt.Println("  " + yellow("!") + " " + s) }
func errLine(s string)  { fmt.Println("  " + red("✘") + " " + s) }
func infoLine(s string) { fmt.Println("  " + dim("•") + " " + s) }

// visibleLen counts printable runes, ignoring ANSI escape sequences.
func visibleLen(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case esc:
			if r == 'm' {
				esc = false
			}
		case r == '\x1b':
			esc = true
		default:
			n++
		}
	}
	return n
}

// shortenLeft keeps the tail of s so it fits in n runes ("…\deep\folder").
func shortenLeft(s string, n int) string {
	if n <= 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-n+1:])
}

func padRight(s string, n int) string {
	if pad := n - visibleLen(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// statusLine redraws a single progress line: a colored prefix plus a plain
// tail (usually a path) that is shortened to fit the console width.
func statusLine(prefix, tail string) {
	w := consoleWidth() - 1
	line := prefix + shortenLeft(tail, w-visibleLen(prefix))
	fmt.Print("\r" + padRight(line, w))
}

func clearStatus() {
	fmt.Print("\r" + strings.Repeat(" ", consoleWidth()-1) + "\r")
}

func readLine(prompt string) (string, bool) {
	fmt.Print(prompt)
	s, err := stdin.ReadString('\n')
	if err != nil && s == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func pause(msg string) {
	fmt.Print("\n  " + dim(msg))
	stdin.ReadString('\n')
}

func box(lines []string, inner int) {
	fmt.Println()
	fmt.Println("  " + green("╭"+strings.Repeat("─", inner+2)+"╮"))
	for _, l := range lines {
		fmt.Println("  " + green("│") + " " + padRight(l, inner) + " " + green("│"))
	}
	fmt.Println("  " + green("╰"+strings.Repeat("─", inner+2)+"╯"))
}

func printBanner() {
	box([]string{
		bgreen(appName) + " " + dim("v"+appVersion),
		"Find and inspect every local Git repository",
		"",
		"by " + bold(authorName) + dim("  ·  ") + authorSite,
		dim("https://") + authorGitHub,
	}, 50)
	fmt.Println()
}

func humanBytes(b uint64) string {
	const gb = 1 << 30
	if b >= 1024*gb {
		return fmt.Sprintf("%.1f TB", float64(b)/float64(1024*gb))
	}
	return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
}

func thousands(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
