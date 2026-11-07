package main

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Desempenho em tempo real: só coleta enquanto alguém está com a tela do PC aberta no painel.
// O painel liga pelo long-poll (live_seconds) e cada envio de amostras renova o prazo.

type sample struct {
	T         int64     `json:"t"` // ms
	CPU       float64   `json:"cpu"`
	Cores     []float64 `json:"cores,omitempty"`
	MemUsed   uint64    `json:"mem_used"`
	MemTotal  uint64    `json:"mem_total"`
	DiskRead  float64   `json:"disk_read"`  // bytes/s
	DiskWrite float64   `json:"disk_write"` // bytes/s
	NetRecv   float64   `json:"net_recv"`   // bytes/s
	NetSent   float64   `json:"net_sent"`   // bytes/s
	Procs     int       `json:"procs"`
	Top       []procTop `json:"top,omitempty"`
}

type procTop struct {
	PID  int32   `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  uint64  `json:"mem"`
	User string  `json:"user,omitempty"`
}

var (
	liveMu    sync.Mutex
	liveUntil time.Time
	liveOn    bool
)

func (a *agent) startLive(seconds int) {
	if seconds > 120 {
		seconds = 120
	}
	liveMu.Lock()
	defer liveMu.Unlock()
	if until := time.Now().Add(time.Duration(seconds) * time.Second); until.After(liveUntil) {
		liveUntil = until
	}
	if liveOn {
		return
	}
	liveOn = true
	logf("desempenho em tempo real: ligado")
	go a.liveLoop()
}

func (a *agent) liveLoop() {
	defer func() {
		liveMu.Lock()
		liveOn = false
		liveMu.Unlock()
		logf("desempenho em tempo real: desligado")
	}()
	s := newSampler()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for i := 0; ; i++ {
		<-tick.C
		liveMu.Lock()
		done := time.Now().After(liveUntil)
		liveMu.Unlock()
		if done {
			return
		}
		smp := s.take(i%3 == 0) // processos a cada 3 s (mais caro)
		var resp struct {
			LiveSeconds int `json:"live_seconds"`
		}
		if err := a.call("POST", "/api/agent/metrics", a.st.Token, smp, &resp); err != nil {
			logf("desempenho: %v", err)
			continue
		}
		liveMu.Lock()
		if resp.LiveSeconds <= 0 {
			liveUntil = time.Now() // ninguém mais olhando
		} else if until := time.Now().Add(time.Duration(resp.LiveSeconds) * time.Second); until.After(liveUntil) {
			liveUntil = until
		}
		liveMu.Unlock()
	}
}

type sampler struct {
	last      time.Time
	disk      [2]uint64
	net       [2]uint64
	procs     map[int32]*process.Process
	numCPU    float64
	firstTake bool
}

func newSampler() *sampler {
	n, _ := cpu.Counts(true)
	if n < 1 {
		n = 1
	}
	s := &sampler{procs: map[int32]*process.Process{}, numCPU: float64(n), firstTake: true}
	_, _ = cpu.Percent(0, false) // primeira leitura só define a base
	_, _ = cpu.Percent(0, true)
	s.disk = diskTotals()
	s.net = netTotals()
	s.last = time.Now()
	return s
}

func (s *sampler) take(withProcs bool) sample {
	now := time.Now()
	dt := now.Sub(s.last).Seconds()
	if dt <= 0 {
		dt = 1
	}
	smp := sample{T: now.UnixMilli()}
	if v, err := cpu.Percent(0, false); err == nil && len(v) > 0 {
		smp.CPU = round1(v[0])
	}
	if v, err := cpu.Percent(0, true); err == nil {
		for _, c := range v {
			smp.Cores = append(smp.Cores, round1(c))
		}
	}
	if m, err := mem.VirtualMemory(); err == nil {
		smp.MemUsed, smp.MemTotal = m.Used, m.Total
	}
	d := diskTotals()
	smp.DiskRead = rate(d[0], s.disk[0], dt)
	smp.DiskWrite = rate(d[1], s.disk[1], dt)
	s.disk = d
	n := netTotals()
	smp.NetRecv = rate(n[0], s.net[0], dt)
	smp.NetSent = rate(n[1], s.net[1], dt)
	s.net = n
	s.last = now
	if withProcs {
		smp.Top, smp.Procs = s.topProcesses()
	}
	return smp
}

func (s *sampler) topProcesses() ([]procTop, int) {
	list, err := process.Processes()
	if err != nil {
		return nil, 0
	}
	alive := map[int32]bool{}
	var out []procTop
	for _, p := range list {
		alive[p.Pid] = true
		if p.Pid == 0 {
			continue
		}
		cached, ok := s.procs[p.Pid]
		if !ok {
			s.procs[p.Pid] = p
			_, _ = p.Percent(0) // base para a próxima leitura
			continue
		}
		pct, err := cached.Percent(0)
		if err != nil {
			continue
		}
		name, _ := cached.Name()
		if strings.EqualFold(name, "System Idle Process") {
			continue
		}
		var rss uint64
		if mi, err := cached.MemoryInfo(); err == nil {
			rss = mi.RSS
		}
		out = append(out, procTop{PID: p.Pid, Name: name, CPU: round1(pct / s.numCPU), Mem: rss})
	}
	for pid := range s.procs {
		if !alive[pid] {
			delete(s.procs, pid)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].Mem > out[j].Mem
	})
	if len(out) > 8 {
		out = out[:8]
	}
	for i := range out {
		if p, ok := s.procs[out[i].PID]; ok {
			out[i].User, _ = p.Username()
		}
	}
	return out, len(list)
}

func diskTotals() [2]uint64 {
	var r [2]uint64
	io, err := disk.IOCounters()
	if err != nil {
		return r
	}
	for name, c := range io {
		// Só discos físicos/volumes de letra, sem contar partições duas vezes.
		if strings.HasPrefix(strings.ToLower(name), "loop") || strings.HasPrefix(strings.ToLower(name), "ram") {
			continue
		}
		r[0] += c.ReadBytes
		r[1] += c.WriteBytes
	}
	return r
}

func netTotals() [2]uint64 {
	var r [2]uint64
	io, err := gnet.IOCounters(false)
	if err != nil || len(io) == 0 {
		return r
	}
	return [2]uint64{io[0].BytesRecv, io[0].BytesSent}
}

func rate(cur, prev uint64, dt float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
