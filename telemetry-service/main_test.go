package main

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		bytes uint64
		want  string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tc := range cases {
		got := formatBytes(tc.bytes)
		if got != tc.want {
			t.Errorf("formatBytes(%d) = %s; want %s", tc.bytes, got, tc.want)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		secs uint64
		want string
	}{
		{45, "0m 45s"},
		{125, "2m 5s"},
		{3665, "1h 1m 5s"},
		{90000, "1d 1h 0m"},
	}

	for _, tc := range cases {
		got := formatUptime(tc.secs)
		if got != tc.want {
			t.Errorf("formatUptime(%d) = %s; want %s", tc.secs, got, tc.want)
		}
	}
}

func TestTelemetryMonitor(t *testing.T) {
	m := newTelemetryMonitor()
	snap := m.GetSnapshot()

	if snap.CPU.LogicalCores <= 0 {
		t.Errorf("Expected logical cores > 0, got %d", snap.CPU.LogicalCores)
	}
	if snap.Memory.TotalBytes == 0 {
		t.Errorf("Expected total memory > 0, got 0")
	}
}
