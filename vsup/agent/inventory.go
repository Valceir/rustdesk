package main

import (
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

type Inventory struct {
	CollectedAt  time.Time   `json:"collected_at"`
	AgentVersion string      `json:"agent_version"`
	Hostname     string      `json:"hostname"`
	LoggedUser   string      `json:"logged_user,omitempty"`
	BootTime     time.Time   `json:"boot_time"`
	UptimeSec    uint64      `json:"uptime_sec"`
	OS           OSInfo      `json:"os"`
	Computer     Computer    `json:"computer"`
	CPU          CPUInfo     `json:"cpu"`
	Memory       MemoryInfo  `json:"memory"`
	Volumes      []Volume    `json:"volumes"`
	Disks        []PhysDisk  `json:"disks"`
	GPUs         []GPU       `json:"gpus"`
	Network      []NetIface  `json:"network"`
	Antivirus    []Antivirus `json:"antivirus"`
	Programs     []Program   `json:"programs"`
	Errors       []string    `json:"errors,omitempty"`
}

type OSInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Build       string `json:"build"`
	Arch        string `json:"arch"`
	InstallDate string `json:"install_date,omitempty"`
}

type Computer struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Serial       string `json:"serial"`
	Board        string `json:"board"`
	BoardSerial  string `json:"board_serial"`
	BIOS         string `json:"bios"`
	Domain       string `json:"domain,omitempty"`
}

type CPUInfo struct {
	Model   string  `json:"model"`
	Cores   int     `json:"cores"`
	Threads int     `json:"threads"`
	MHz     float64 `json:"mhz"`
}

type MemoryInfo struct {
	Total   uint64      `json:"total"`
	Used    uint64      `json:"used"`
	Modules []MemModule `json:"modules"`
}

type MemModule struct {
	Capacity     uint64 `json:"capacity"`
	Speed        uint32 `json:"speed"`
	Manufacturer string `json:"manufacturer"`
	PartNumber   string `json:"part_number"`
	Slot         string `json:"slot"`
}

type Volume struct {
	Mount string `json:"mount"`
	Label string `json:"label,omitempty"`
	FS    string `json:"fs"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

type PhysDisk struct {
	Model  string `json:"model"`
	Size   uint64 `json:"size"`
	Type   string `json:"type"` // SSD, HDD
	Bus    string `json:"bus,omitempty"`
	Serial string `json:"serial,omitempty"`
	Health string `json:"health,omitempty"`
}

type GPU struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
}

type NetIface struct {
	Name    string   `json:"name"`
	MAC     string   `json:"mac"`
	IPv4    []string `json:"ipv4"`
	IPv6    []string `json:"ipv6,omitempty"`
	Gateway string   `json:"gateway,omitempty"`
	DNS     []string `json:"dns,omitempty"`
	DHCP    bool     `json:"dhcp"`
}

type Antivirus struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Updated bool   `json:"updated"`
}

type Program struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	InstallDate string `json:"install_date,omitempty"`
}

func collectInventory() *Inventory {
	inv := &Inventory{CollectedAt: time.Now().UTC(), AgentVersion: Version}
	fail := func(what string, err error) {
		if err != nil {
			inv.Errors = append(inv.Errors, what+": "+err.Error())
		}
	}

	inv.Hostname, _ = os.Hostname()
	if h, err := host.Info(); err == nil {
		inv.BootTime = time.Unix(int64(h.BootTime), 0).UTC()
		inv.UptimeSec = h.Uptime
		inv.OS = OSInfo{Name: h.Platform, Version: h.PlatformVersion, Build: h.KernelVersion, Arch: h.KernelArch}
	} else {
		fail("host", err)
	}

	if c, err := cpu.Info(); err == nil && len(c) > 0 {
		inv.CPU.Model = strings.TrimSpace(c[0].ModelName)
		inv.CPU.MHz = c[0].Mhz
	} else {
		fail("cpu", err)
	}
	inv.CPU.Cores, _ = cpu.Counts(false)
	inv.CPU.Threads, _ = cpu.Counts(true)

	if m, err := mem.VirtualMemory(); err == nil {
		inv.Memory.Total, inv.Memory.Used = m.Total, m.Used
	} else {
		fail("memória", err)
	}

	if parts, err := disk.Partitions(false); err == nil {
		for _, p := range parts {
			u, err := disk.Usage(p.Mountpoint)
			if err != nil || u.Total == 0 {
				continue
			}
			inv.Volumes = append(inv.Volumes, Volume{Mount: p.Mountpoint, FS: p.Fstype, Total: u.Total, Used: u.Used, Free: u.Free})
		}
	} else {
		fail("volumes", err)
	}

	if ifs, err := gnet.Interfaces(); err == nil {
		for _, i := range ifs {
			if i.HardwareAddr == "" || hasFlag(i.Flags, "loopback") || !hasFlag(i.Flags, "up") {
				continue
			}
			n := NetIface{Name: i.Name, MAC: strings.ToUpper(i.HardwareAddr)}
			for _, a := range i.Addrs {
				ip, _, err := net.ParseCIDR(a.Addr)
				if err != nil || ip.IsLinkLocalUnicast() {
					continue
				}
				if ip.To4() != nil {
					n.IPv4 = append(n.IPv4, ip.String())
				} else {
					n.IPv6 = append(n.IPv6, ip.String())
				}
			}
			if len(n.IPv4) > 0 || len(n.IPv6) > 0 {
				inv.Network = append(inv.Network, n)
			}
		}
	} else {
		fail("rede", err)
	}

	collectPlatform(inv) // WMI e registro (Windows)

	sort.Slice(inv.Programs, func(i, j int) bool {
		return strings.ToLower(inv.Programs[i].Name) < strings.ToLower(inv.Programs[j].Name)
	})
	return inv
}

func hasFlag(flags []string, f string) bool {
	for _, x := range flags {
		if strings.EqualFold(x, f) {
			return true
		}
	}
	return false
}
