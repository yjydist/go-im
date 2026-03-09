package logger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit_Success(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	err := Init("info", logFile, 10, 3, 7)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if L == nil {
		t.Fatal("expected L to be set after Init, got nil")
	}

	// 验证日志文件被创建（写一条日志触发文件创建）
	L.Info("test message")
	_ = L.Sync()

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Errorf("expected log file to be created at %s", logFile)
	}
}

func TestInit_DefaultValues(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "defaults.log")

	// maxSize=0, maxBackups=0, maxAge=0 应回退到默认值
	err := Init("warn", logFile, 0, 0, 0)
	if err != nil {
		t.Fatalf("Init with zero params failed: %v", err)
	}
	if L == nil {
		t.Fatal("expected L to be set after Init, got nil")
	}
}

func TestInit_NegativeValues(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "negative.log")

	// 负数也应回退到默认值
	err := Init("debug", logFile, -1, -5, -10)
	if err != nil {
		t.Fatalf("Init with negative params failed: %v", err)
	}
	if L == nil {
		t.Fatal("expected L to be set after Init, got nil")
	}
}

func TestInit_InvalidLevel(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "badlevel.log")

	// 无效 level 应回退到 debug
	err := Init("invalid_level", logFile, 10, 3, 7)
	if err != nil {
		t.Fatalf("Init with invalid level failed: %v", err)
	}
	if L == nil {
		t.Fatal("expected L to be set after Init, got nil")
	}
}
