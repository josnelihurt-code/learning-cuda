package domain

import (
	"fmt"
	"net"
	"time"
)

type DeviceStatus struct {
	LastSeen  time.Time
	Power     float64
	Voltage   float64
	Version   string
	Module    string
	Hostname  string
	IPAddress string
	IsOnline  bool
	LWTStatus string
}

func (ds *DeviceStatus) String() string {
	maskedIP := formatIPAddress(ds.IPAddress)
	lwtStatus := ds.LWTStatus
	if lwtStatus == "" {
		lwtStatus = "Unknown"
	}
	return fmt.Sprintf("Power: %.2f W, Voltage: %.1f V, Version: %s, Module: %s, Hostname: %s, IP: %s, Status: %s",
		ds.Power, ds.Voltage, ds.Version, ds.Module, ds.Hostname, maskedIP, lwtStatus)
}

func formatIPAddress(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "XXX.XXX.XXX.XXX"
	}

	ipv4 := parsedIP.To4()
	if ipv4 == nil {
		return "XXX.XXX.XXX.XXX"
	}

	return fmt.Sprintf("XXX.XXX.XXX.%d", ipv4[3])
}

func NewDeviceStatus() *DeviceStatus {
	return &DeviceStatus{
		IsOnline: false,
	}
}

// Clone returns a deep copy; all fields are value types, so a struct copy suffices.
func (ds *DeviceStatus) Clone() *DeviceStatus {
	clone := *ds
	return &clone
}

func (ds *DeviceStatus) UpdatePower(power float64, timestamp time.Time) {
	ds.Power = power
	ds.LastSeen = timestamp
	ds.IsOnline = true
}

func (ds *DeviceStatus) UpdateVoltage(voltage float64) {
	ds.Voltage = voltage
}

func (ds *DeviceStatus) UpdateLWTStatus(status string) {
	ds.LWTStatus = status
	ds.IsOnline = (status == "Online")
}

func (ds *DeviceStatus) UpdateInfo1(version, module string) {
	ds.Version = version
	ds.Module = module
}

func (ds *DeviceStatus) UpdateInfo2(hostname, ipAddress string) {
	ds.Hostname = hostname
	ds.IPAddress = ipAddress
}
