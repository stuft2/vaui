package secret

import "time"

// Value is one readable version of a KV v2 secret.
type Value struct {
	Data         map[string]any
	Version      int
	CreatedTime  time.Time
	DeletionTime *time.Time
	Destroyed    bool
}

// Version describes one entry in a KV v2 secret's history.
type Version struct {
	Version      int
	CreatedTime  time.Time
	DeletionTime *time.Time
	Destroyed    bool
}

// Metadata describes the version history of a KV v2 secret.
type Metadata struct {
	CurrentVersion int
	Versions       []Version
}
