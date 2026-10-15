package main

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Everything (voidtools) keeps an index of every file on NTFS drives. When it
// is running we ask it for all ".git" entries over its IPC protocol (window
// messages, see everything_ipc.h in the Everything SDK) instead of walking
// the disks, which turns a minutes-long scan into well under a second.

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmUser              = 0x0400
	wmCopyData          = 0x004A
	evIsDBLoaded        = 401
	evCopyDataQuery2W   = 18
	evMatchPath         = 0x00000004
	evRequestFullPath   = 0x00000004
	evSortPathAscending = 3
	evReplyID           = 0x4746      // "GF", our id for the reply message
	hwndMessage         = ^uintptr(2) // HWND_MESSAGE (-3)
)

type copyDataStruct struct {
	dwData uintptr
	cbData uint32
	lpData uintptr
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      [2]int32
}

func everythingWindow() uintptr {
	cls, _ := syscall.UTF16PtrFromString("EVERYTHING_TASKBAR_NOTIFICATION")
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	return h
}

// everythingReady reports whether Everything is running with its index loaded.
func everythingReady() bool {
	h := everythingWindow()
	if h == 0 {
		return false
	}
	r, _, _ := procSendMessageW.Call(h, wmUser, evIsDBLoaded, 0)
	return r != 0
}

var (
	replyData  []byte
	replyClass *uint16
	wndProc    = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		if msg == wmCopyData {
			cds := (*copyDataStruct)(winPtr(lParam))
			if cds.dwData == evReplyID {
				// The buffer is only valid inside this call, so copy it.
				replyData = append([]byte(nil), unsafe.Slice((*byte)(winPtr(cds.lpData)), cds.cbData)...)
				return 1
			}
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	})
)

// everythingQuery runs one search and returns full paths of the results.
func everythingQuery(search string, flags uint32, maxResults uint32) ([]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ev := everythingWindow()
	if ev == 0 {
		return nil, errors.New("Everything is not running")
	}
	inst, _, _ := procGetModuleHandleW.Call(0)
	if replyClass == nil {
		replyClass, _ = syscall.UTF16PtrFromString("GitFinderEverythingReply")
		wc := wndClassEx{lpfnWndProc: wndProc, hInstance: inst, lpszClassName: replyClass}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(replyClass)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, inst, 0)
	if hwnd == 0 {
		return nil, err
	}
	defer procDestroyWindow.Call(hwnd)

	// EVERYTHING_IPC_QUERY2 header followed by the UTF-16 search string.
	text := utf16.Encode([]rune(search + "\x00"))
	buf := make([]byte, 28+2*len(text))
	le := binary.LittleEndian
	le.PutUint32(buf[0:], uint32(hwnd))
	le.PutUint32(buf[4:], evReplyID)
	le.PutUint32(buf[8:], flags)
	le.PutUint32(buf[12:], 0) // offset
	le.PutUint32(buf[16:], maxResults)
	le.PutUint32(buf[20:], evRequestFullPath)
	le.PutUint32(buf[24:], evSortPathAscending)
	for i, c := range text {
		le.PutUint16(buf[28+2*i:], c)
	}
	cds := copyDataStruct{dwData: evCopyDataQuery2W, cbData: uint32(len(buf)), lpData: uintptr(unsafe.Pointer(&buf[0]))}

	replyData = nil
	if r, _, _ := procSendMessageW.Call(ev, wmCopyData, hwnd, uintptr(unsafe.Pointer(&cds))); r == 0 {
		return nil, errors.New("Everything rejected the query")
	}
	runtime.KeepAlive(buf)

	// The reply arrives as a WM_COPYDATA sent to our window.
	deadline := time.Now().Add(15 * time.Second)
	var m winMsg
	for replyData == nil {
		if time.Now().After(deadline) {
			return nil, errors.New("Everything did not answer in time")
		}
		for {
			r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1) // PM_REMOVE
			if r == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		if replyData == nil {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return parseEverythingList(replyData)
}

// parseEverythingList decodes EVERYTHING_IPC_LIST2: a 20 byte header, then
// numitems x {flags, data_offset}; each data block is a DWORD length and the
// UTF-16 path.
func parseEverythingList(b []byte) ([]string, error) {
	le := binary.LittleEndian
	if len(b) < 20 {
		return nil, errors.New("short reply from Everything")
	}
	n := int(le.Uint32(b[4:]))
	var out []string
	for i := 0; i < n; i++ {
		item := 20 + 8*i
		if item+8 > len(b) {
			break
		}
		off := int(le.Uint32(b[item+4:]))
		if off+4 > len(b) {
			continue
		}
		size := int(le.Uint32(b[off:]))
		start, end := off+4, off+4+2*size
		if end > len(b) {
			continue
		}
		chars := make([]uint16, size)
		for j := range chars {
			chars[j] = le.Uint16(b[start+2*j:])
		}
		out = append(out, string(utf16.Decode(chars)))
	}
	return out, nil
}

// everythingIndexed reports whether Everything knows anything under root.
// Drives Everything does not index (FAT USB sticks, network shares) are
// then scanned the normal way.
func everythingIndexed(root string) bool {
	res, err := everythingQuery(`"`+strings.TrimSuffix(root, `\`)+`\"`, evMatchPath, 1)
	return err == nil && len(res) > 0
}

// everythingRepos returns repositories under roots using the Everything
// index, applying the same skip rules as the disk walker.
func everythingRepos(roots []string) ([]string, error) {
	hits, err := everythingQuery("wfn:.git", 0, 0xFFFFFFFF)
	if err != nil {
		return nil, err
	}
	var repos []string
	seen := map[string]bool{}
	for _, h := range hits {
		dir := filepath.Dir(h)
		key := repoKey(dir)
		if seen[key] || !underAny(key, roots) || skippedPath(dir) {
			continue
		}
		fi, err := os.Lstat(h)
		if err != nil {
			continue // stale index entry
		}
		if !isGitMarker(dir, fs.FileInfoToDirEntry(fi)) {
			continue
		}
		seen[key] = true
		repos = append(repos, dir)
	}
	return dropNested(repos), nil
}

// skippedPath applies the walker's skip rules to every folder of a path.
func skippedPath(dir string) bool {
	vol := filepath.VolumeName(dir)
	parts := strings.Split(strings.Trim(dir[len(vol):], `\`), `\`)
	parent := vol + `\`
	for i, p := range parts {
		if p == "" {
			continue
		}
		if shouldSkip(parent, p, i == 0 && len(vol) == 2) {
			return true
		}
		parent = filepath.Join(parent, p)
	}
	return false
}

// dropNested removes repos inside other found repos (submodules, vendored
// clones); the walker never descends into a repository either.
func dropNested(repos []string) []string {
	found := map[string]bool{}
	for _, r := range repos {
		found[repoKey(r)] = true
	}
	var out []string
	for _, r := range repos {
		nested := false
		for p := filepath.Dir(r); p != filepath.Dir(p); p = filepath.Dir(p) {
			if found[repoKey(p)] {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, r)
		}
	}
	return out
}

// winPtr turns an address handed to us by Windows (not Go memory) into a
// pointer.
func winPtr(addr uintptr) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer(nil), addr)
}
