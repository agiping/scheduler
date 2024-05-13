package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

func Init(logLevel string) {
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		fmt.Println("Invalid log level provided:", logLevel)
		fmt.Println("Supported log levels: info, debug, warn, error")
		os.Exit(1)
	}

	Log = logrus.New()
	Log.SetLevel(level)

	Log.SetOutput(os.Stderr)

	Log.SetFormatter(&logrus.TextFormatter{
		DisableColors:   false,
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		// PadLevelText:    true, // Ensure level text is right-padded with spaces
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := filepath.Base(f.File)
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", filename, f.Line)
		},
	})
	Log.SetReportCaller(true)
}
