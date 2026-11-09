package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func isService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

type handler struct{}

func (handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(stop)
	}()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for c := range req {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			close(stop)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
			}
			return false, 0
		}
	}
	return false, 0
}

func runService() {
	if err := svc.Run(ServiceName, handler{}); err != nil {
		logf("serviço: %v", err)
	}
}

func installService() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	if s, err := m.OpenService(ServiceName); err == nil {
		// Reinstalação: atualiza o caminho e reinicia.
		stopService(s)
		cfg, err := s.Config()
		if err == nil {
			cfg.BinaryPathName = fmt.Sprintf(`"%s"`, exe)
			_ = s.UpdateConfig(cfg)
		}
		_ = s.Start()
		s.Close()
		return nil
	}
	s, err := m.CreateService(ServiceName, exe, mgr.Config{
		DisplayName:  ServiceDisplay,
		Description:  ServiceDesc,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	})
	if err != nil {
		return err
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24*60*60)
	return s.Start()
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return nil // não instalado
	}
	defer s.Close()
	stopService(s)
	return s.Delete()
}

func stopService(s *mgr.Service) {
	st, err := s.Control(svc.Stop)
	if err != nil {
		return
	}
	for i := 0; i < 20 && st.State != svc.Stopped; i++ {
		time.Sleep(500 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return
		}
	}
}

// machineGUID identifica a instalação do Windows; impede outro PC de assumir o mesmo ID.
func machineGUID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("MachineGuid")
	return v
}

// Onde o programa (VDesk ou o antigo VSUP) guarda a configuração, com o ID, no Windows.
func configCandidatesFor(app string) []string {
	win := os.Getenv("SystemRoot")
	if win == "" {
		win = `C:\Windows`
	}
	rel := filepath.Join("AppData", "Roaming", app, "config", app+".toml")
	c := []string{
		filepath.Join(win, `ServiceProfiles\LocalService`, rel),
		filepath.Join(win, `System32\config\systemprofile`, rel),
	}
	if d, err := os.UserConfigDir(); err == nil {
		c = append(c, filepath.Join(d, app, "config", app+".toml"))
	}
	return c
}

func logDir() string { return logDirFor(AppName) }

func logDirFor(app string) string {
	d := os.Getenv("ProgramData")
	if d == "" {
		d = `C:\ProgramData`
	}
	return filepath.Join(d, app)
}
