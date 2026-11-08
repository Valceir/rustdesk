module vsup-agent

go 1.24.0

replace golang.org/x/sys => github.com/golang/sys v0.38.0

require (
	github.com/shirou/gopsutil/v4 v4.25.10
	github.com/yusufpapurcu/wmi v1.2.4
	golang.org/x/crypto v0.43.0
	golang.org/x/sys v0.38.0
)

require (
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
)

replace (
	golang.org/x/xerrors => github.com/golang/xerrors v0.0.0-20191204190536-9bdfabe68543
	gopkg.in/yaml.v3 => github.com/go-yaml/yaml/v3 v3.0.1
)

replace golang.org/x/crypto => github.com/golang/crypto v0.43.0
