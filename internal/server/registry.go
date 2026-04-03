package server

import (
	"sync"
)

type Registry struct {
	mu     sync.RWMutex
	client *YggdrasilHandler
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Set(client *YggdrasilHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client = client
}

func (r *Registry) Get() *YggdrasilHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client = nil
}
