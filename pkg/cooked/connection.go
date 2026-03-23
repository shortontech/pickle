package cooked

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ManagedConnection wraps a *sql.DB with in-flight query tracking and
// graceful retirement. When a connection's DSN changes on config reload,
// the old pool is retired and closes itself after its last in-flight
// query completes.
type ManagedConnection struct {
	DB       *sql.DB
	Name     string
	inflight atomic.Int64
	done     chan struct{} // closed when inflight hits 0 after retirement
	retired  atomic.Bool
}

// Acquire increments the in-flight counter. Returns false if the connection
// is retired — caller must re-resolve from the ManagedConnections map.
func (mc *ManagedConnection) Acquire() bool {
	if mc.retired.Load() {
		return false
	}
	mc.inflight.Add(1)
	return true
}

// Release decrements the in-flight counter. If the connection is retired
// and this was the last in-flight query, the pool is closed.
func (mc *ManagedConnection) Release() {
	if mc.inflight.Add(-1) == 0 && mc.retired.Load() {
		close(mc.done)
	}
}

// ManagedConnections holds named database connections with atomic swap support.
// Keyed by connection name from config/database.go.
var ManagedConnections = &connectionMap{m: map[string]*ManagedConnection{}}

type connectionMap struct {
	mu sync.RWMutex
	m  map[string]*ManagedConnection
}

// Load returns the ManagedConnection for the given name, or nil.
func (cm *connectionMap) Load(name string) *ManagedConnection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.m[name]
}

// Store sets a ManagedConnection by name.
func (cm *connectionMap) Store(name string, mc *ManagedConnection) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.m[name] = mc
}

// Swap atomically replaces the connection for the given name and returns the old one.
func (cm *connectionMap) Swap(name string, mc *ManagedConnection) *ManagedConnection {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	old := cm.m[name]
	cm.m[name] = mc
	return old
}

// Names returns all connection names.
func (cm *connectionMap) Names() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	names := make([]string, 0, len(cm.m))
	for name := range cm.m {
		names = append(names, name)
	}
	return names
}

// WrapConnection wraps an existing *sql.DB as a ManagedConnection and stores it.
func WrapConnection(name string, db *sql.DB) {
	mc := &ManagedConnection{
		DB:   db,
		Name: name,
		done: make(chan struct{}),
	}
	ManagedConnections.Store(name, mc)
}

// acquireConnection resolves a named connection from ManagedConnections,
// retrying if the connection was retired between Load and Acquire.
func acquireConnection(name string) *ManagedConnection {
	for {
		mc := ManagedConnections.Load(name)
		if mc == nil {
			return nil
		}
		if mc.Acquire() {
			return mc
		}
		// Connection was retired between Load and Acquire — re-resolve.
		runtime.Gosched()
	}
}

// swapConnection opens a new pool with the given DSN, health-checks it,
// swaps it into ManagedConnections, and retires the old pool.
func swapConnection(name, driver, dsn string) error {
	newDB, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("connection %q: failed to open: %w", name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := newDB.PingContext(ctx); err != nil {
		newDB.Close()
		return fmt.Errorf("connection %q: health check failed: %w", name, err)
	}

	newConn := &ManagedConnection{DB: newDB, Name: name, done: make(chan struct{})}
	old := ManagedConnections.Swap(name, newConn)

	if old != nil {
		old.retired.Store(true)
		if old.inflight.Load() == 0 {
			close(old.done)
		}
		go func() {
			<-old.done
			old.DB.Close()
			log.Printf("pickle: connection pool retired: %s", name)
		}()
	}

	return nil
}
