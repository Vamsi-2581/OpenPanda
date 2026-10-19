//go:build windows

// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows folder chooser is COM: CLSID_FileOpenDialog created with
// FOS_PICKFOLDERS is the modern Explorer-style browser — breadcrumb bar, quick
// access, "New folder", the sidebar — and it is the same control Explorer's own
// "Select Folder" button raises. The obvious alternative, WinForms'
// FolderBrowserDialog (what a PowerShell one-liner reaches for), is the 1990s
// tree dialog *and* costs a ~1 s powershell.exe spawn on every click.
//
// There is no cgo in this repo, so the vtable is walked by hand: every method
// is called through its slot index in the interface's table.

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	// FOS_PICKFOLDERS is the whole point: without it the same object is the
	// file picker. The other two keep the result to a real filesystem folder.
	fosPickFolders     = 0x00000020
	fosForceFileSystem = 0x00000040
	fosPathMustExist   = 0x00000800

	// SIGDN_FILESYSPATH asks IShellItem for a plain drive-letter path.
	sigdnFileSystemPath = 0x80058000

	// HRESULT_FROM_WIN32(ERROR_CANCELLED) — the user pressed Cancel or Esc.
	hrCancelled uint32 = 0x800704C7
	// RPC_E_CHANGED_MODE: this thread already lives in another apartment. Not
	// fatal — the dialog is attempted anyway and reports for itself.
	rpcEChangedMode uint32 = 0x80010106
)

var (
	modOle32   = windows.NewLazySystemDLL("ole32.dll")
	modShell32 = windows.NewLazySystemDLL("shell32.dll")
	modUser32  = windows.NewLazySystemDLL("user32.dll")

	procCoInitializeEx              = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize              = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance            = modOle32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = modOle32.NewProc("CoTaskMemFree")
	procSHCreateItemFromParsingName = modShell32.NewProc("SHCreateItemFromParsingName")
	procGetForegroundWindow         = modUser32.NewProc("GetForegroundWindow")
)

var (
	clsidFileOpenDialog = windows.GUID{
		Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE,
		Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7},
	}
	iidIFileOpenDialog = windows.GUID{
		Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768,
		Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60},
	}
	iidIShellItem = windows.GUID{
		Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE,
		Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE},
	}
)

// ifileOpenDialog and its vtable. The order is the interface's inheritance
// chain — IUnknown, IModalWindow, IFileDialog, IFileOpenDialog — and every
// slot must stay in place, including the ones never called.
type ifileOpenDialog struct {
	vtbl *ifileOpenDialogVtbl
}

type ifileOpenDialogVtbl struct {
	QueryInterface      uintptr
	AddRef              uintptr
	Release             uintptr
	Show                uintptr // IModalWindow
	SetFileTypes        uintptr // IFileDialog
	SetFileTypeIndex    uintptr
	GetFileTypeIndex    uintptr
	Advise              uintptr
	Unadvise            uintptr
	SetOptions          uintptr
	GetOptions          uintptr
	SetDefaultFolder    uintptr
	SetFolder           uintptr
	GetFolder           uintptr
	GetCurrentSelection uintptr
	SetFileName         uintptr
	GetFileName         uintptr
	SetTitle            uintptr
	SetOkButtonLabel    uintptr
	SetFileNameLabel    uintptr
	GetResult           uintptr
	AddPlace            uintptr
	SetDefaultExtension uintptr
	Close               uintptr
	SetClientGuid       uintptr
	ClearClientData     uintptr
	SetFilter           uintptr
	GetResults          uintptr // IFileOpenDialog
	GetSelectedItems    uintptr
}

// ishellItem is the folder handle the dialog speaks in.
type ishellItem struct {
	vtbl *ishellItemVtbl
}

type ishellItemVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	BindToHandler  uintptr
	GetParent      uintptr
	GetDisplayName uintptr
	GetAttributes  uintptr
	Compare        uintptr
}

// hresult narrows the register-width return value to the signed 32-bit code
// COM actually promises.
func hresult(r uintptr) int32 { return int32(uint32(r)) }

func (d *ifileOpenDialog) release() {
	if d == nil || d.vtbl == nil {
		return
	}
	_, _, _ = syscall.SyscallN(d.vtbl.Release, uintptr(unsafe.Pointer(d)))
}

func (i *ishellItem) release() {
	if i == nil || i.vtbl == nil {
		return
	}
	_, _, _ = syscall.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i)))
}

func (d *ifileOpenDialog) setOptions(flags uint32) error {
	hr, _, _ := syscall.SyscallN(d.vtbl.SetOptions, uintptr(unsafe.Pointer(d)), uintptr(flags))
	if h := hresult(hr); h < 0 {
		return fmt.Errorf("IFileDialog::SetOptions: 0x%08X", uint32(h))
	}
	return nil
}

func (d *ifileOpenDialog) getOptions() (uint32, error) {
	var opts uint32
	hr, _, _ := syscall.SyscallN(d.vtbl.GetOptions, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&opts)))
	if h := hresult(hr); h < 0 {
		return 0, fmt.Errorf("IFileDialog::GetOptions: 0x%08X", uint32(h))
	}
	return opts, nil
}

func (d *ifileOpenDialog) setTitle(title string) error {
	p, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(d.vtbl.SetTitle, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(p)))
	if h := hresult(hr); h < 0 {
		return fmt.Errorf("IFileDialog::SetTitle: 0x%08X", uint32(h))
	}
	return nil
}

func (d *ifileOpenDialog) setFolder(item *ishellItem) error {
	hr, _, _ := syscall.SyscallN(d.vtbl.SetFolder, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(item)))
	if h := hresult(hr); h < 0 {
		return fmt.Errorf("IFileDialog::SetFolder: 0x%08X", uint32(h))
	}
	return nil
}

// resultPath asks the dialog for the folder the user settled on.
func (d *ifileOpenDialog) resultPath() (string, error) {
	var item *ishellItem
	hr, _, _ := syscall.SyscallN(d.vtbl.GetResult, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&item)))
	if h := hresult(hr); h < 0 || item == nil {
		return "", fmt.Errorf("IFileDialog::GetResult: 0x%08X", uint32(hresult(hr)))
	}
	defer item.release()

	var name *uint16
	hr, _, _ = syscall.SyscallN(item.vtbl.GetDisplayName, uintptr(unsafe.Pointer(item)), sigdnFileSystemPath, uintptr(unsafe.Pointer(&name)))
	if h := hresult(hr); h < 0 || name == nil {
		return "", fmt.Errorf("IShellItem::GetDisplayName: 0x%08X", uint32(hresult(hr)))
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))

	path := windows.UTF16PtrToString(name)
	if path == "" {
		return "", errors.New("the chooser returned an empty path")
	}
	return path, nil
}

// newShellItem wraps a filesystem path in the IShellItem the dialog needs to
// start somewhere. A path it cannot parse simply yields nil — the chooser then
// opens wherever it likes, which is better than refusing to open.
func newShellItem(path string) *ishellItem {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var item *ishellItem
	hr, _, _ := procSHCreateItemFromParsingName.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&iidIShellItem)),
		uintptr(unsafe.Pointer(&item)),
	)
	if hresult(hr) < 0 {
		return nil
	}
	return item
}

// newFileOpenDialog creates the shell's open-dialog object already configured
// as a folder picker. The caller owns the thread and has initialized COM.
func newFileOpenDialog() (*ifileOpenDialog, error) {
	var dlg *ifileOpenDialog
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dlg)),
	)
	if h := hresult(hr); h < 0 || dlg == nil {
		return nil, fmt.Errorf("CoCreateInstance(FileOpenDialog): 0x%08X", uint32(hresult(hr)))
	}
	if err := dlg.setOptions(fosPickFolders | fosForceFileSystem | fosPathMustExist); err != nil {
		dlg.release()
		return nil, err
	}
	return dlg, nil
}

// openNativeDirectoryDialog raises Explorer's own folder chooser.
func openNativeDirectoryDialog(initial, title string) (string, bool, error) {
	// The dialog is apartment-threaded and runs its own modal loop, so this
	// goroutine must stay on one OS thread for the whole call — Go would
	// otherwise migrate it mid-window and orphan the message pump.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	initHR := hresult(hr)
	if initHR < 0 && uint32(initHR) != rpcEChangedMode {
		return "", false, fmt.Errorf("CoInitializeEx: 0x%08X", uint32(initHR))
	}
	// Balance only an initialization this call actually performed (S_OK, not
	// S_FALSE — "already initialized").
	if initHR >= 0 {
		defer procCoUninitialize.Call()
	}

	dlg, err := newFileOpenDialog()
	if err != nil {
		return "", false, err
	}
	defer dlg.release()

	if title != "" {
		_ = dlg.setTitle(title) // a caption is cosmetic; never fail the pick for it
	}
	if folder := existingDir(initial); folder != "" {
		if item := newShellItem(folder); item != nil {
			_ = dlg.setFolder(item)
			item.release()
		}
	}

	// Owning the dialog to whatever window is in front (the browser that made
	// the request) is what brings it up over the console instead of behind it.
	owner, _, _ := procGetForegroundWindow.Call()
	hr, _, _ = syscall.SyscallN(dlg.vtbl.Show, uintptr(unsafe.Pointer(dlg)), owner)
	switch h := hresult(hr); uint32(h) {
	case 0: // S_OK — a folder was chosen
	case hrCancelled:
		return "", true, nil
	default:
		return "", false, fmt.Errorf("IFileDialog::Show: 0x%08X", uint32(h))
	}

	path, err := dlg.resultPath()
	if err != nil {
		return "", false, err
	}
	return path, false, nil
}
