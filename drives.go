package main

import (
	"syscall"
	"unsafe"
)

var (
	procGetLogicalDrives      = kernel32.NewProc("GetLogicalDrives")
	procGetDriveTypeW         = kernel32.NewProc("GetDriveTypeW")
	procGetDiskFreeSpaceExW   = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetVolumeInformationW = kernel32.NewProc("GetVolumeInformationW")
)

type Drive struct {
	Root     string
	Label    string
	Kind     string
	Total    uint64
	Free     uint64
	Ready    bool
	Selected bool
}

// listDrives returns every mounted drive letter. Fixed drives that are ready
// are selected by default; removable and network drives are opt-in.
func listDrives() []Drive {
	mask, _, _ := procGetLogicalDrives.Call()
	var out []Drive
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, _ := syscall.UTF16PtrFromString(root)
		d := Drive{Root: root}

		t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p)))
		switch t {
		case 2:
			d.Kind = "Removable"
		case 3:
			d.Kind = "Fixed"
		case 4:
			d.Kind = "Network"
		case 5:
			d.Kind = "CD/DVD"
		case 6:
			d.Kind = "RAM disk"
		default:
			continue
		}

		var free, total, totalFree uint64
		r, _, _ := procGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(p)),
			uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
		if r != 0 {
			d.Ready, d.Total, d.Free = true, total, totalFree
			label := make([]uint16, 261)
			r, _, _ = procGetVolumeInformationW.Call(uintptr(unsafe.Pointer(p)),
				uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)), 0, 0, 0, 0, 0)
			if r != 0 {
				d.Label = syscall.UTF16ToString(label)
			}
		}
		d.Selected = d.Ready && d.Kind == "Fixed"
		out = append(out, d)
	}
	return out
}

func selectedRoots(drives []Drive) []string {
	var roots []string
	for _, d := range drives {
		if d.Selected && d.Ready {
			roots = append(roots, d.Root)
		}
	}
	return roots
}

// applySelection restores the drive selection saved from the previous run.
func applySelection(drives []Drive, saved []string) {
	if len(saved) == 0 {
		return
	}
	want := map[string]bool{}
	for _, s := range saved {
		want[s] = true
	}
	for i := range drives {
		drives[i].Selected = drives[i].Ready && want[drives[i].Root]
	}
}
