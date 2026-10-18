// Package core defines the lifecycle interface for the embedded proxy core.
package core

import "context"

// Core starts and stops the embedded proxy engine. Start receives the state
// directory and the effective configuration document.
type Core interface {
	Start(ctx context.Context, dir string, cfg []byte) error
	Stop() error
}

// Stub is a placeholder Core used until mihomo is integrated. It accepts a
// configuration without doing anything.
type Stub struct{}

// Start implements Core.
func (Stub) Start(context.Context, string, []byte) error {
	return nil
}

// Stop implements Core.
func (Stub) Stop() error {
	return nil
}

// New returns the placeholder core for this stage.
func New() Core {
	return Stub{}
}
