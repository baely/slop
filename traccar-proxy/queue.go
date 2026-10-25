package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Job is one captured client request, replayed against a target.
type Job struct {
	Received    time.Time `json:"received"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`  // path after the token prefix; "" or "/..."
	Query       string    `json:"query"` // raw query string
	ContentType string    `json:"content_type,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	Body        []byte    `json:"body,omitempty"`
}

var seq atomic.Uint64

// Queue is a durable FIFO of jobs for one target: one JSON file per job,
// named so lexical order is arrival order.
type Queue struct {
	dir     string
	max     int
	mu      sync.Mutex
	names   []string
	wake    chan struct{}
	dropped atomic.Uint64
}

func openQueue(dir string, max int) (*Queue, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	q := &Queue{dir: dir, max: max, wake: make(chan struct{}, 1)}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			q.names = append(q.names, e.Name())
		}
	}
	sort.Strings(q.names)
	return q, nil
}

// Push persists a job and wakes the worker. It never blocks on delivery.
func (q *Queue) Push(j *Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%020d-%06d.json", j.Received.UnixNano(), seq.Add(1)%1_000_000)
	tmp := filepath.Join(q.dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(q.dir, name)); err != nil {
		return err
	}

	q.mu.Lock()
	q.names = append(q.names, name)
	var evict []string
	if over := len(q.names) - q.max; over > 0 {
		evict = append(evict, q.names[:over]...)
		q.names = q.names[over:]
	}
	q.mu.Unlock()

	for _, n := range evict {
		os.Remove(filepath.Join(q.dir, n))
		q.dropped.Add(1)
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// Peek returns the oldest job without removing it, or nil if empty.
func (q *Queue) Peek() (*Job, string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.names) > 0 {
		name := q.names[0]
		data, err := os.ReadFile(filepath.Join(q.dir, name))
		if err != nil {
			// Evicted or corrupt on disk; skip it.
			q.names = q.names[1:]
			continue
		}
		var j Job
		if err := json.Unmarshal(data, &j); err != nil {
			os.Remove(filepath.Join(q.dir, name))
			q.names = q.names[1:]
			continue
		}
		return &j, name, nil
	}
	return nil, "", nil
}

// Ack removes a job by name once it has been delivered (or given up on).
func (q *Queue) Ack(name string) {
	q.mu.Lock()
	if len(q.names) > 0 && q.names[0] == name {
		q.names = q.names[1:]
	} else {
		for i, n := range q.names {
			if n == name {
				q.names = append(q.names[:i], q.names[i+1:]...)
				break
			}
		}
	}
	q.mu.Unlock()
	os.Remove(filepath.Join(q.dir, name))
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.names)
}

// Wake is signalled whenever a job is pushed.
func (q *Queue) Wake() <-chan struct{} { return q.wake }
