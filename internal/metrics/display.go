package metrics

import (
	"io"
	"os"
	"sync/atomic"

	"github.com/bmvkrd/livelog"
)

var globalDisplay atomic.Pointer[livelog.Display]

// SetDisplay registers the shared Display used by the ConsoleConnector.
// Must be called before any engine is created.
func SetDisplay(d *livelog.Display) {
	globalDisplay.Store(d)
}

// GetDisplay returns the shared Display, or nil if none has been set.
func GetDisplay() *livelog.Display {
	return globalDisplay.Load()
}

// Writer returns an io.Writer that routes through the global Display.
// Falls back to os.Stdout if no Display has been set.
func Writer() io.Writer {
	if d := globalDisplay.Load(); d != nil {
		return d.Writer()
	}
	return os.Stdout
}
