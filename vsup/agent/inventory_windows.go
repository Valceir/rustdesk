package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"
)

type win32OS struct {
	Caption        string
	Version        string
	BuildNumber    string
	OSArchitecture string
	InstallDate    time.Time
}

type win32CS struct {
	Manufacturer string
	Model        string
	UserName     string
	Domain       string
	PartOfDomain bool
}

type win32Board struct {
	Manufacturer string
	Product      string
	SerialNumber string
}

type win32BIOS struct {
	Manufacturer      string
	SMBIOSBIOSVersion string
	SerialNumber      string
}

type win32Mem struct {
	Capacity      uint64
	Speed         uint32
	Manufacturer  string
	PartNumber    string
	DeviceLocator string
}

type win32GPU struct {
	Name          string
	DriverVersion string
}

type win32LogicalDisk struct {
	DeviceID   string
	VolumeName string
}

type win32NetCfg struct {
	MACAddress           string
	DefaultIPGateway     []string
	DNSServerSearchOrder []string
	DHCPEnabled          bool
}

type msftPhysicalDisk struct {
	FriendlyName string
	Size         uint64
	MediaType    uint16
	BusType      uint16
	SerialNumber string
	HealthStatus uint16
}

type win32DiskDrive struct {
	Model         string
	Size          uint64
	InterfaceType string
	SerialNumber  string
}

type avProduct struct {
	DisplayName  string
	ProductState uint32
}

func collectPlatform(inv *Inventory) {
	q := func(what string, dst any, where string, ns ...string) bool {
		var err error
		query := wmi.CreateQuery(dst, where)
		if len(ns) > 0 {
			err = wmi.QueryNamespace(query, dst, ns[0])
		} else {
			err = wmi.Query(query, dst)
		}
		if err != nil {
			inv.Errors = append(inv.Errors, what+": "+err.Error())
			return false
		}
		return true
	}

	var oss []win32OS
	if q("sistema", &oss, "") && len(oss) > 0 {
		o := oss[0]
		inv.OS.Name = strings.TrimSpace(o.Caption)
		inv.OS.Version = o.Version
		if dv := regString(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "DisplayVersion"); dv != "" {
			inv.OS.Version = dv + " (" + o.Version + ")"
		}
		inv.OS.Build = o.BuildNumber
		inv.OS.Arch = o.OSArchitecture
		if !o.InstallDate.IsZero() {
			inv.OS.InstallDate = o.InstallDate.Format("2006-01-02")
		}
	}

	var cs []win32CS
	if q("computador", &cs, "") && len(cs) > 0 {
		inv.Computer.Manufacturer = strings.TrimSpace(cs[0].Manufacturer)
		inv.Computer.Model = strings.TrimSpace(cs[0].Model)
		inv.LoggedUser = cs[0].UserName
		if cs[0].PartOfDomain {
			inv.Computer.Domain = cs[0].Domain
		}
	}
	var board []win32Board
	if q("placa-mãe", &board, "") && len(board) > 0 {
		inv.Computer.Board = strings.TrimSpace(board[0].Manufacturer + " " + board[0].Product)
		inv.Computer.BoardSerial = clean(board[0].SerialNumber)
	}
	var bios []win32BIOS
	if q("bios", &bios, "") && len(bios) > 0 {
		inv.Computer.BIOS = strings.TrimSpace(bios[0].Manufacturer + " " + bios[0].SMBIOSBIOSVersion)
		inv.Computer.Serial = clean(bios[0].SerialNumber)
	}

	var mems []win32Mem
	if q("pentes de memória", &mems, "") {
		for _, m := range mems {
			inv.Memory.Modules = append(inv.Memory.Modules, MemModule{
				Capacity: m.Capacity, Speed: m.Speed, Manufacturer: clean(m.Manufacturer),
				PartNumber: clean(m.PartNumber), Slot: m.DeviceLocator,
			})
		}
	}

	var gpus []win32GPU
	if q("vídeo", &gpus, "") {
		for _, g := range gpus {
			inv.GPUs = append(inv.GPUs, GPU{Name: g.Name, Driver: g.DriverVersion})
		}
	}

	var lds []win32LogicalDisk
	if q("rótulos", &lds, "WHERE DriveType = 3") {
		for i := range inv.Volumes {
			for _, l := range lds {
				if strings.EqualFold(strings.TrimRight(inv.Volumes[i].Mount, `\`), l.DeviceID) {
					inv.Volumes[i].Label = l.VolumeName
				}
			}
		}
	}

	var pds []msftPhysicalDisk
	if err := wmi.QueryNamespace(wmi.CreateQuery(&pds, ""), &pds, `root\Microsoft\Windows\Storage`); err == nil && len(pds) > 0 {
		for _, d := range pds {
			inv.Disks = append(inv.Disks, PhysDisk{
				Model: strings.TrimSpace(d.FriendlyName), Size: d.Size, Type: mediaType(d.MediaType),
				Bus: busType(d.BusType), Serial: clean(d.SerialNumber), Health: health(d.HealthStatus),
			})
		}
	} else {
		var dds []win32DiskDrive
		if q("discos", &dds, "") {
			for _, d := range dds {
				inv.Disks = append(inv.Disks, PhysDisk{Model: d.Model, Size: d.Size, Bus: d.InterfaceType, Serial: clean(d.SerialNumber)})
			}
		}
	}

	var ncs []win32NetCfg
	if q("configuração de rede", &ncs, "WHERE IPEnabled = TRUE") {
		for i := range inv.Network {
			for _, c := range ncs {
				if strings.EqualFold(strings.ReplaceAll(c.MACAddress, "-", ":"), inv.Network[i].MAC) {
					if len(c.DefaultIPGateway) > 0 {
						inv.Network[i].Gateway = c.DefaultIPGateway[0]
					}
					inv.Network[i].DNS = c.DNSServerSearchOrder
					inv.Network[i].DHCP = c.DHCPEnabled
				}
			}
		}
	}

	// Só existe em edições de estação de trabalho (não em Windows Server).
	var avs []avProduct
	if err := wmi.QueryNamespace(wmi.CreateQuery(&avs, ""), &avs, `root\SecurityCenter2`); err == nil {
		for _, a := range avs {
			inv.Antivirus = append(inv.Antivirus, Antivirus{
				Name: a.DisplayName, Enabled: a.ProductState&0x1000 != 0, Updated: a.ProductState&0x10 == 0,
			})
		}
	}

	inv.Programs = installedPrograms()
}

func installedPrograms() []Program {
	seen := map[string]bool{}
	var out []Program
	read := func(root registry.Key, path string, access uint32) {
		k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|access)
		if err != nil {
			return
		}
		defer k.Close()
		names, _ := k.ReadSubKeyNames(-1)
		for _, n := range names {
			sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE|access)
			if err != nil {
				continue
			}
			name, _, _ := sk.GetStringValue("DisplayName")
			sys, _, _ := sk.GetIntegerValue("SystemComponent")
			parent, _, _ := sk.GetStringValue("ParentKeyName")
			ver, _, _ := sk.GetStringValue("DisplayVersion")
			pub, _, _ := sk.GetStringValue("Publisher")
			date, _, _ := sk.GetStringValue("InstallDate")
			sk.Close()
			name = strings.TrimSpace(name)
			if name == "" || sys == 1 || parent != "" {
				continue
			}
			key := strings.ToLower(name + "|" + ver)
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(date) == 8 {
				date = date[:4] + "-" + date[4:6] + "-" + date[6:]
			} else {
				date = ""
			}
			out = append(out, Program{Name: name, Version: ver, Publisher: strings.TrimSpace(pub), InstallDate: date})
		}
	}
	const up = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	read(registry.LOCAL_MACHINE, up, registry.WOW64_64KEY)
	read(registry.LOCAL_MACHINE, up, registry.WOW64_32KEY)
	// Programas instalados só para um usuário (perfis carregados)
	if users, err := registry.OpenKey(registry.USERS, "", registry.ENUMERATE_SUB_KEYS); err == nil {
		sids, _ := users.ReadSubKeyNames(-1)
		users.Close()
		for _, sid := range sids {
			if strings.HasPrefix(sid, "S-1-5-21-") && !strings.HasSuffix(sid, "_Classes") {
				read(registry.USERS, sid+`\`+up, 0)
			}
		}
	}
	return out
}

func regString(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "default string", "to be filled by o.e.m.", "system serial number", "none", "0", "00000000", "not specified", "unknown":
		return ""
	}
	return s
}

func mediaType(t uint16) string {
	switch t {
	case 3:
		return "HDD"
	case 4:
		return "SSD"
	case 5:
		return "SCM"
	}
	return ""
}

func busType(t uint16) string {
	m := map[uint16]string{1: "SCSI", 3: "ATA", 7: "USB", 8: "RAID", 10: "SAS", 11: "SATA", 12: "SD", 15: "Virtual", 17: "NVMe"}
	if v, ok := m[t]; ok {
		return v
	}
	return fmt.Sprint(t)
}

func health(h uint16) string {
	switch h {
	case 0:
		return "OK"
	case 1:
		return "Alerta"
	case 2:
		return "Com falha"
	}
	return ""
}
