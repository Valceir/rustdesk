//go:build darwin

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// collectPlatform completa o inventário com o system_profiler e utilitários do macOS.
func collectPlatform(inv *Inventory) {
	inv.Platform = "mac"
	fail := func(what string, err error) {
		if err != nil {
			inv.Errors = append(inv.Errors, what+": "+err.Error())
		}
	}

	inv.OS.Name = "macOS"
	if v := cmdOut("sw_vers", "-productVersion"); v != "" {
		inv.OS.Version = v
	}
	if b := cmdOut("sw_vers", "-buildVersion"); b != "" {
		inv.OS.Build = b
	}
	if u := cmdOut("stat", "-f", "%Su", "/dev/console"); u != "" && u != "root" {
		inv.LoggedUser = u
	}

	var hw struct {
		SPHardwareDataType []struct {
			MachineName  string `json:"machine_name"`
			MachineModel string `json:"machine_model"`
			ModelNumber  string `json:"model_number"`
			Serial       string `json:"serial_number"`
			Chip         string `json:"chip_type"`
			CPUType      string `json:"cpu_type"`
			BootROM      string `json:"boot_rom_version"`
		} `json:"SPHardwareDataType"`
		SPDisplaysDataType []struct {
			Model  string `json:"sppci_model"`
			Name   string `json:"_name"`
			Vendor string `json:"spdisplays_vendor"`
		} `json:"SPDisplaysDataType"`
		SPStorageDataType []struct {
			Name     string `json:"_name"`
			Mount    string `json:"mount_point"`
			FS       string `json:"file_system"`
			Size     uint64 `json:"size_in_bytes"`
			Free     uint64 `json:"free_space_in_bytes"`
			Physical struct {
				Device   string `json:"device_name"`
				Medium   string `json:"medium_type"`
				Protocol string `json:"protocol"`
				Internal string `json:"is_internal_disk"`
				SMART    string `json:"smart_status"`
			} `json:"physical_drive"`
		} `json:"SPStorageDataType"`
	}
	if err := profiler(&hw, "SPHardwareDataType", "SPDisplaysDataType", "SPStorageDataType"); err != nil {
		fail("system_profiler", err)
	}
	if len(hw.SPHardwareDataType) > 0 {
		h := hw.SPHardwareDataType[0]
		inv.Computer.Manufacturer = "Apple"
		inv.Computer.Model = strings.TrimSpace(h.MachineName + " (" + h.MachineModel + ")")
		inv.Computer.Serial = h.Serial
		inv.Computer.BIOS = h.BootROM
		if inv.CPU.Model == "" {
			inv.CPU.Model = firstNonEmpty(h.Chip, h.CPUType)
		}
	}
	for _, d := range hw.SPDisplaysDataType {
		inv.GPUs = append(inv.GPUs, GPU{Name: firstNonEmpty(d.Model, d.Name)})
	}

	// Volumes do APFS: o Mac mostra um "Macintosh HD" (o espaço livre é do container todo) e os discos externos.
	var vols []Volume
	seenDisk := map[string]bool{}
	for _, s := range hw.SPStorageDataType {
		if s.Mount != "/" && !strings.HasPrefix(s.Mount, "/Volumes/") {
			continue
		}
		if s.Size > 0 {
			vols = append(vols, Volume{Mount: s.Mount, Label: s.Name, FS: s.FS, Total: s.Size, Used: s.Size - min(s.Free, s.Size), Free: s.Free})
		}
		p := s.Physical
		if p.Device != "" && !seenDisk[p.Device] {
			seenDisk[p.Device] = true
			d := PhysDisk{Model: p.Device, Size: s.Size, Bus: p.Protocol, Health: p.SMART}
			if strings.EqualFold(p.Medium, "ssd") {
				d.Type = "SSD"
			} else if strings.EqualFold(p.Medium, "rotational") {
				d.Type = "HDD"
			}
			inv.Disks = append(inv.Disks, d)
		}
	}
	if len(vols) > 0 {
		inv.Volumes = vols
	}

	collectMacNetwork(inv)

	var apps struct {
		SPApplicationsDataType []struct {
			Name     string `json:"_name"`
			Version  string `json:"version"`
			Path     string `json:"path"`
			Obtained string `json:"obtained_from"`
			Modified string `json:"lastModified"`
			Info     string `json:"info"`
		} `json:"SPApplicationsDataType"`
	}
	if err := profiler(&apps, "SPApplicationsDataType"); err != nil {
		fail("programas", err)
	}
	for _, a := range apps.SPApplicationsDataType {
		// Só o que está em Aplicativos: ignora utilitários do sistema e apps internos de outros pacotes.
		if !strings.HasPrefix(a.Path, "/Applications/") && !strings.Contains(a.Path, "/Users/") {
			continue
		}
		if strings.Contains(strings.TrimPrefix(a.Path, "/Applications/"), ".app/") {
			continue
		}
		p := Program{Name: a.Name, Version: a.Version}
		if a.Obtained == "apple" {
			p.Publisher = "Apple"
		}
		if t, err := time.Parse(time.RFC3339, a.Modified); err == nil {
			p.InstallDate = t.Format("2006-01-02")
		}
		inv.Programs = append(inv.Programs, p)
	}
}

func collectMacNetwork(inv *Inventory) {
	route := cmdOut("route", "-n", "get", "default")
	var gw, iface string
	for _, line := range strings.Split(route, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "gateway":
			gw = strings.TrimSpace(v)
		case "interface":
			iface = strings.TrimSpace(v)
		}
	}
	var dns []string
	sc := bufio.NewScanner(strings.NewReader(cmdOut("scutil", "--dns")))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "resolver #2") {
			break
		}
		if strings.HasPrefix(line, "nameserver[") {
			if _, v, ok := strings.Cut(line, ":"); ok {
				dns = append(dns, strings.TrimSpace(v))
			}
		}
	}
	for i := range inv.Network {
		n := &inv.Network[i]
		if n.Name != iface {
			continue
		}
		n.Gateway, n.DNS = gw, dns
		n.DHCP = cmdOut("ipconfig", "getpacket", iface) != ""
	}
}

func profiler(out any, types ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b, err := exec.CommandContext(ctx, "/usr/sbin/system_profiler", append([]string{"-json"}, types...)...).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func cmdOut(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
