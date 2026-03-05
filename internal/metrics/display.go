package metrics

import (
	"io"
	"os"

	"github.com/bmvkrd/livelog"
)

var globalDisplay *livelog.Display

// SetDisplay registers the shared Display used by the ConsoleConnector.
// Must be called before any engine is created.
func SetDisplay(d *livelog.Display) {
	globalDisplay = d
}

// GetDisplay returns the shared Display, or nil if none has been set.
func GetDisplay() *livelog.Display {
	return globalDisplay
}

// Writer returns an io.Writer that routes through the global Display.
// Falls back to os.Stdout if no Display has been set.
func Writer() io.Writer {
	if globalDisplay != nil {
		return globalDisplay.Writer()
	}
	return os.Stdout
}
