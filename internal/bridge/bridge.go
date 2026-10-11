// Package bridge lets paneltest build a panel the way package panel does,
// without panel exporting its internals: panel fills in the function when it
// is initialised, and paneltest imports panel, so it is always set.
package bridge

import (
	"github.com/AlexK-Notable/herdr-panel/internal/ui"
	"github.com/AlexK-Notable/herdr-panel/spec"
)

// UIOptions turns a spec and a panel.Options, passed as any, into the
// panel's internal options, exactly as panel.Run does.
var UIOptions func(s *spec.Spec, opts any) (ui.Options, error)
