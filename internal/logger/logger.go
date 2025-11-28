package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.SugaredLogger

// Init initializes the global logger.
// If logPath is provided, logs are written to that file (overwriting it).
// Otherwise, they are written to stdout.
func Init(verbose bool, logPath string) {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
	encoderConfig.EncodeCaller = nil

	// If writing to file, remove color codes from the text
	if logPath != "" {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	}

	logLevel := zap.InfoLevel
	if verbose {
		logLevel = zap.DebugLevel
	}

	var writer zapcore.WriteSyncer
	if logPath != "" {
		// O_TRUNC ensures we rewrite the file, not append
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			// Fallback if file creation fails
			writer = zapcore.AddSync(os.Stdout)
			println("Failed to create log file: " + err.Error())
		} else {
			writer = zapcore.AddSync(f)
		}
	} else {
		writer = zapcore.AddSync(os.Stdout)
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		writer,
		logLevel,
	)

	logger := zap.New(core)
	Log = logger.Sugar()
}

// Sync flushes any buffered log entries.
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
