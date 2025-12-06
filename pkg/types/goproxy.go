package types

import (
	"time"
)

// GoProxyInfo represents the .info file format for a module version
type GoProxyInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}
