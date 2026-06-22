package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SalzDevs/agenttop/internal/event"
)

type Store struct {
	mu       sync.Mutex
	events   []event.Event
	cap      int
	nextID   int64
	logFile  *os.File
	enc      *json.Encoder

	totalCost float64
	totalIn   int
	totalOut  int
	totalReqs int

	inFlight map[int64]bool
}

func New(path string, cap int) (*Store, error) {
	s := &Store{cap: cap, inFlight: make(map[int64]bool)}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		s.logFile = f
		s.enc = json.NewEncoder(f)
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.logFile != nil {
		return s.logFile.Close()
	}
	return nil
}

func (s *Store) Append(e event.Event) event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	e.ID = s.nextID
	if e.Time.IsZero() {
		e.Time = time.Now()
	}

	s.events = append(s.events, e)
	if len(s.events) > s.cap {
		s.events = s.events[len(s.events)-s.cap:]
	}

	if e.InFlight() {
		s.inFlight[e.TraceID] = true
	} else {
		delete(s.inFlight, e.TraceID)
		s.totalReqs++
		s.totalCost += e.CostUSD
		s.totalIn += e.InputTokens
		s.totalOut += e.OutputTokens
	}

	if s.enc != nil {
		_ = s.enc.Encode(e)
	}
	return e
}

func (s *Store) Recent(n int) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.events) {
		n = len(s.events)
	}
	out := make([]event.Event, n)
	copy(out, s.events[len(s.events)-n:])
	return out
}

func (s *Store) Stats() (cost float64, in, out, reqs, inFlight int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalCost, s.totalIn, s.totalOut, s.totalReqs, len(s.inFlight)
}
