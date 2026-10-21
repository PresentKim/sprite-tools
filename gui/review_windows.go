//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"sprite-tools/pkg/i18n"
)

// Flags and operation code of SHFileOperationW (shellapi.h).
const (
	foDelete           = 0x0003
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

// shFileOpStruct mirrors SHFILEOPSTRUCTW with the natural alignment of 64-bit Windows.
type shFileOpStruct struct {
	Hwnd                 uintptr
	Func                 uint32
	From                 *uint16
	To                   *uint16
	Flags                uint16
	AnyOperationsAborted int32
	NameMappings         uintptr
	ProgressTitle        *uint16
}

var procSHFileOperation = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

// recycleFiles moves absolute file paths to the Recycle Bin (FOF_ALLOWUNDO). FOF_WANTNUKEWARNING keeps Windows from
// silently destroying a file it cannot recycle (too large, drive without a bin): it asks first instead.
func recycleFiles(paths []string) error {
	var list []uint16
	for _, p := range paths {
		u, err := syscall.UTF16FromString(p)
		if err != nil {
			continue
		}
		list = append(list, u...) // u already ends with its NUL
	}
	if len(list) == 0 {
		return nil
	}
	list = append(list, 0) // the list is double-NUL terminated
	op := shFileOpStruct{
		Func:  foDelete,
		From:  &list[0],
		Flags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
	}
	r, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return i18n.Errorf("err.gui.recycle_failed", int(r))
	}
	if op.AnyOperationsAborted != 0 {
		return i18n.Errorf("err.gui.recycle_failed", -1)
	}
	return nil
}
