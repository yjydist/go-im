package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/lumberjack.v2"
)

var L *zap.Logger

// Init 初始化 Zap 日志。
// maxSize: 单个日志文件最大 MB（<=0 默认 100）；maxBackups: 保留旧文件数（<=0 默认 5）；maxAge: 保留天数（<=0 默认 30）。
func Init(level string, filename string, maxSize, maxBackups, maxAge int) error {
	// 应用默认值
	if maxSize <= 0 {
		maxSize = 100
	}
	if maxBackups <= 0 {
		maxBackups = 5
	}
	if maxAge <= 0 {
		maxAge = 30
	}

	// 解析日志级别
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.DebugLevel
	}

	// 编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 文件输出（带日志切割）
	fileWriter := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   true,
	}

	// 多输出：控制台 + 文件
	cores := []zapcore.Core{
		zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(os.Stdout),
			zapLevel,
		),
		zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			zapcore.AddSync(fileWriter),
			zapLevel,
		),
	}

	core := zapcore.NewTee(cores...)
	L = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(0))

	return nil
}

// Sync 刷新缓冲日志
func Sync() {
	if L != nil {
		_ = L.Sync()
	}
}
