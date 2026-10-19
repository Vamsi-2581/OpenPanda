// SPDX-License-Identifier: AGPL-3.0-or-later

package hwinfo

import "testing"

func TestClassifyAppleModel(t *testing.T) {
	cases := []struct{ model, want string }{
		{"MacBookPro18,3", FormLaptop},
		{"MacBookAir10,1", FormLaptop},
		{"Mac14,12", ""}, // a bare "Mac14,12" is a Mac mini, but the id alone
		// does not say so — the Macmini* ids do.
		{"Macmini9,1", FormMini},
		{"MacPro7,1", FormDesktop},
		{"iMac21,1", FormDesktop},
		{"MacStudio1,1", FormDesktop},
		{"VirtualMac2,1", FormVM},
		{"", ""},
	}
	for _, c := range cases {
		if got := classifyAppleModel(c.model); got != c.want {
			t.Errorf("classifyAppleModel(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}

func TestClassifyLinuxChassis(t *testing.T) {
	cases := []struct {
		name    string
		chassis string
		prod    string
		vendor  string
		wants   string
	}{
		{"laptop chassis", "9", "20XW00ABUS", "LENOVO", FormLaptop},
		{"notebook chassis", "10\n", "XPS 13", "Dell Inc.", FormLaptop},
		{"tower chassis", "7", "OptiPlex 7080", "Dell Inc.", FormDesktop},
		{"mini pc chassis", "35", "NUC10i7FNH", "Intel", FormMini},
		{"server chassis", "23", "PowerEdge R740", "Dell Inc.", FormServer},
		{"mini by name", "", "B550 MINI ITX", "ASRock", FormMini},
		{"virtualbox guest", "1", "VirtualBox", "innotek GmbH", FormVM},
		{"qemu guest", "3", "Standard PC (Q35)", "QEMU", FormVM},
		{"nothing known", "", "", "", ""},
	}
	for _, c := range cases {
		if got := classifyLinuxChassis(c.chassis, c.prod, c.vendor); got != c.wants {
			t.Errorf("%s: classifyLinuxChassis(%q, %q, %q) = %q, want %q",
				c.name, c.chassis, c.prod, c.vendor, got, c.wants)
		}
	}
}

func TestClassifyWindowsSystem(t *testing.T) {
	cases := []struct{ out, want string }{
		{"2|LENOVO|20XW00ABUS", FormLaptop},
		{"10|ASUSTeK|ZenBook UX325", FormLaptop},
		{"1|Dell Inc.|OptiPlex 7080", FormDesktop},
		{"3|Hewlett-Packard|Z4 G4 Workstation", FormDesktop},
		{"4|Dell Inc.|PowerEdge R740", FormServer},
		{"1|VMware, Inc.|VMware Virtual Platform", FormVM},
		{"1|innotek GmbH|VirtualBox", FormVM},
		{"|Intel|NUC10i7FNH", FormMini},
		{"|ASUS|ROG Strix G15", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := classifyWindowsSystem(c.out); got != c.want {
			t.Errorf("classifyWindowsSystem(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}
