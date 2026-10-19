// SPDX-License-Identifier: AGPL-3.0-or-later

package hwinfo

import (
	"runtime"
	"strings"
	"sync"
)

// Form factors — the machine classes the console draws distinct glyphs for.
// They answer "what kind of box is this", which the capability card cannot:
// a card carries a CPU model and a resource class, and neither distinguishes
// a laptop from a tower. Advertised alongside the OS in the capability
// summary so a peer's topology map can draw the right device.
const (
	FormLaptop  = "laptop"  // clamshell: MacBook, notebook, 2-in-1
	FormDesktop = "desktop" // tower / all-in-one / workstation
	FormMini    = "mini"    // Mac mini, NUC, mini PC, space-saving chassis
	FormServer  = "server"  // rack / blade / main server chassis
	FormVM      = "vm"      // virtual machine (the hypervisor's guest)
)

var (
	formOnce  sync.Once
	formValue string
)

// FormFactor returns this machine's form factor, or "" when no probe on this
// platform answered. Probed once per process: the value is a property of the
// hardware, and the probes shell out (PowerShell on Windows), so a heartbeat
// must never pay for them twice.
func FormFactor() string {
	formOnce.Do(func() { formValue = detectFormFactor() })
	return formValue
}

func detectFormFactor() string {
	switch runtime.GOOS {
	case "darwin":
		return detectAppleFormFactor()
	case "linux":
		return classifyLinuxChassis(
			readFile("/sys/class/dmi/id/chassis_type"),
			readFile("/sys/class/dmi/id/product_name"),
			readFile("/sys/class/dmi/id/sys_vendor"),
		)
	case "windows":
		// One CIM round-trip answers both questions: PCSystemType is the
		// chassis class Windows itself believes, Manufacturer/Model expose
		// the hypervisor when this is a guest. The three values are joined
		// into one line so an empty field cannot shift the parse.
		return classifyWindowsSystem(powershell(
			"$c = Get-CimInstance Win32_ComputerSystem; " +
				"\"$($c.PCSystemType)|$($c.Manufacturer)|$($c.Model)\"",
		))
	}
	return ""
}

// detectAppleFormFactor resolves the Mac's family. hw.model is the cheapest
// answer and enough for the ids that name their family (MacBookPro18,3,
// Macmini9,1, iMac21,1). Newer boards use MacNN,N ids shared across families
// — a MacBook Pro M3 is Mac15,3, a Mac mini M2 Pro is Mac14,12 — so those
// fall through to the marketing name, and finally to the battery, whose
// presence is what separates a clamshell from a desktop.
func detectAppleFormFactor() string {
	// hw.model: "MacBookPro18,3", "Macmini9,1", "MacPro7,1", "VirtualMac2,1".
	if ff := classifyAppleModel(probe("sysctl", "-n", "hw.model")); ff != "" {
		return ff
	}
	lower := strings.ToLower(probe("system_profiler", "SPHardwareDataType"))
	// The values ("Mac mini", "MacBook Pro") are not localized even when the
	// keys are, so a substring match survives a non-English system.
	for _, m := range []struct{ mark, form string }{
		{"macbook", FormLaptop},
		{"mac mini", FormMini},
		{"mac studio", FormDesktop},
		{"imac", FormDesktop},
		{"mac pro", FormDesktop},
	} {
		if strings.Contains(lower, m.mark) {
			return m.form
		}
	}
	if strings.Contains(strings.ToLower(probe("pmset", "-g", "batt")), "internalbattery") {
		return FormLaptop
	}
	return ""
}

// classifyAppleModel maps an Apple board id to a form factor.
func classifyAppleModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ""
	}
	// A guest on Apple's hypervisor reports a VirtualMac board.
	if strings.Contains(m, "virtual") {
		return FormVM
	}
	switch {
	case strings.Contains(m, "macbook"):
		return FormLaptop
	case strings.Contains(m, "macmini"):
		return FormMini
	case strings.Contains(m, "macpro"), strings.Contains(m, "imac"), strings.Contains(m, "macstudio"):
		return FormDesktop
	}
	// A MacNN,N id says nothing about the family — caller falls through to
	// the marketing name rather than guessing "desktop".
	return ""
}

// classifyLinuxChassis maps SMBIOS chassis type + product name to a form
// factor. Chassis type is the primary signal (the firmware's own answer);
// product name catches mini PCs and hypervisor guests, whose chassis type is
// often the generic "Desktop" (3) or unset.
func classifyLinuxChassis(chassisType, productName, vendor string) string {
	name := strings.ToLower(strings.TrimSpace(productName))
	ven := strings.ToLower(strings.TrimSpace(vendor))
	for _, mark := range []string{"virtualbox", "vmware", "qemu", "kvm", "innotek", "bochs", "xen"} {
		if strings.Contains(name, mark) || strings.Contains(ven, mark) {
			return FormVM
		}
	}
	// 8 portable, 9 laptop, 10 notebook, 14 sub-notebook, 31 convertible,
	// 30 tablet — all clamshell/slate classes.
	switch strings.TrimSpace(chassisType) {
	case "8", "9", "10", "14", "30", "31", "32":
		return FormLaptop
	case "17", "23", "28", "29":
		return FormServer
	case "4", "5", "15", "16", "35", "36":
		return FormMini
	case "3", "6", "7", "13", "24":
		return FormDesktop
	}
	for _, mark := range []string{"mini", "nuc", "brix", "box"} {
		if strings.Contains(name, mark) {
			return FormMini
		}
	}
	return ""
}

// classifyWindowsSystem parses the "type|manufacturer|model" answer above.
func classifyWindowsSystem(out string) string {
	fields := strings.Split(strings.TrimSpace(out), "|")
	get := func(i int) string {
		if i >= len(fields) {
			return ""
		}
		return strings.TrimSpace(fields[i])
	}
	pcType := get(0)
	vendor := strings.ToLower(get(1))
	model := strings.ToLower(get(2))
	for _, mark := range []string{"vmware", "virtualbox", "innotek", "qemu", "kvm", "xen", "parallels"} {
		if strings.Contains(vendor, mark) || strings.Contains(model, mark) {
			return FormVM
		}
	}
	// PCSystemType (Win32_ComputerSystem): 1 desktop, 2 mobile, 3 workstation,
	// 4 enterprise server, 5 SOHO server, 6 appliance, 7 performance server,
	// 8 slate, 9 tablet, 10 notebook, 11 handheld.
	switch pcType {
	case "2", "8", "9", "10", "11":
		return FormLaptop
	case "4", "5", "6", "7":
		return FormServer
	case "1", "3":
		return FormDesktop
	}
	// Fall back to the model string, which is often all a VM or a NUC reports.
	for _, mark := range []string{"mini", "nuc", "box"} {
		if strings.Contains(model, mark) {
			return FormMini
		}
	}
	for _, mark := range []string{"laptop", "notebook", "book"} {
		if strings.Contains(model, mark) {
			return FormLaptop
		}
	}
	return ""
}
