package main

import (
	"strings"
	"testing"
)

func TestRenderBar(t *testing.T) {
	bar := renderBar(50.0, 10)
	if !strings.Contains(bar, "50.0%") {
		t.Fatalf("Expected bar to display 50.0%%, got %s", bar)
	}

	barOver := renderBar(120.0, 10)
	if !strings.Contains(barOver, "100.0%") {
		t.Fatalf("Expected bar cap at 100%%, got %s", barOver)
	}

	barZero := renderBar(-5.0, 10)
	if !strings.Contains(barZero, "0.0%") {
		t.Fatalf("Expected bar floor at 0%%, got %s", barZero)
	}
}

func TestTUIFormatters(t *testing.T) {
	bStr := formatBytes(1024 * 1024 * 50)
	if bStr != "50.0 MB" {
		t.Fatalf("Expected 50.0 MB, got %s", bStr)
	}

	uStr := formatUptime(3665)
	if uStr != "1h 1m 5s" {
		t.Fatalf("Expected 1h 1m 5s, got %s", uStr)
	}
}
