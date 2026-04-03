package server

import (
	"sync"
)

// Registry хранит активные соединения
type Registry struct {
	mu        sync.RWMutex
	yggdrasil *YggdrasilHandler
	ratatoskr *RatatoskrHandler
}

func NewRegistry() *Registry {
	return &Registry{}
}

// Yggdrasil
func (r *Registry) SetYggdrasil(client *YggdrasilHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.yggdrasil = client
}

func (r *Registry) GetYggdrasil() *YggdrasilHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.yggdrasil
}

func (r *Registry) ClearYggdrasil() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.yggdrasil = nil
}

// Ratatoskr
func (r *Registry) SetRatatoskr(handler *RatatoskrHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ratatoskr = handler
}

func (r *Registry) GetRatatoskr() *RatatoskrHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ratatoskr
}

func (r *Registry) ClearRatatoskr() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ratatoskr = nil
}
