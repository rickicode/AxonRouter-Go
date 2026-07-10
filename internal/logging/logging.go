package logging

import (
	"log/slog"
	"os"
)

// Logger is the application-wide structured logger.
var Logger *slog.Logger

// Init initialises the global logger. format must be "json" or "text".
func Init(format string) {
	if format == "json" {
		Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	} else {
		Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
}
