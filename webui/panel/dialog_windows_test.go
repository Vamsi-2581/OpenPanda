//go:build windows

// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"runtime"
	"testing"
)

// TestFileOpenDialogWiring drives the real COM object as far as it can without
// showing a window: create it, read back the options, point it at a folder.
// Every call walks the vtable by index, so a slot out of order does not return
// an error — it crashes the process, and this test is where that has to
// happen rather than on someone's click.
func TestFileOpenDialogWiring(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); hresult(hr) < 0 {
		t.Skipf("COM unavailable in this session (0x%08X)", uint32(hresult(hr)))
	}
	defer procCoUninitialize.Call()

	dlg, err := newFileOpenDialog()
	if err != nil {
		t.Skipf("no shell dialog in this session: %v", err)
	}
	defer dlg.release()

	opts, err := dlg.getOptions()
	if err != nil {
		t.Fatalf("GetOptions: %v", err)
	}
	if opts&fosPickFolders == 0 {
		t.Fatalf("FOS_PICKFOLDERS did not stick: options 0x%08X", opts)
	}

	if err := dlg.setTitle("Select Folder"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	dir := t.TempDir()
	item := newShellItem(dir)
	if item == nil {
		t.Fatalf("SHCreateItemFromParsingName(%q) failed", dir)
	}
	defer item.release()
	if err := dlg.setFolder(item); err != nil {
		t.Fatalf("SetFolder: %v", err)
	}
}

func TestExistingDir(t *testing.T) {
	dir := t.TempDir()
	if got := existingDir(dir); got == "" {
		t.Fatalf("existingDir(%q) = %q, want the directory itself", dir, got)
	}
	if got := existingDir(dir + `\definitely-missing`); got != "" {
		t.Fatalf("existingDir on a missing path = %q, want empty", got)
	}
	if got := existingDir(""); got != "" {
		t.Fatalf("existingDir(\"\") = %q, want empty", got)
	}
}
