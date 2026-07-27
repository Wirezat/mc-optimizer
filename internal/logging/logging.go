package logging

import (
	"bufio"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxEntries = 500
	tailSleep  = 200 * time.Millisecond
)

// Entry is a single parsed log line.
type Entry struct {
	Level   string `json:"level"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

// Store is an in-memory ring buffer of log entries with SSE fan-out.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	subs    []chan Entry
}

// Global is the process-wide log store.
var Global = &Store{entries: make([]Entry, 0, maxEntries)}

// parseLine parses GoLog output: [LEVEL] [timestamp] message
func parseLine(line string) (Entry, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || line[0] != '[' {
		return Entry{}, false
	}
	i := strings.IndexByte(line, ']')
	if i < 0 {
		return Entry{}, false
	}
	level, rest := line[1:i], strings.TrimSpace(line[i+1:])
	if len(rest) < 2 || rest[0] != '[' {
		return Entry{}, false
	}
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return Entry{}, false
	}
	msg := ""
	if j+2 < len(rest) {
		msg = rest[j+2:]
	}
	return Entry{Level: level, Time: rest[1:j], Message: msg}, true
}

func (s *Store) add(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= maxEntries {
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:len(s.entries)-1]
	}
	s.entries = append(s.entries, e)
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Recent returns up to n of the most recent entries.
func (s *Store) Recent(n int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.entries)
	if n <= 0 || total == 0 {
		return nil
	}
	if n > total {
		n = total
	}
	out := make([]Entry, n)
	copy(out, s.entries[total-n:])
	return out
}

// Subscribe returns a channel that receives all future entries.
func (s *Store) Subscribe() chan Entry {
	ch := make(chan Entry, 64)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (s *Store) Unsubscribe(ch chan Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Load reads an existing log file into the ring buffer.
func (s *Store) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if e, ok := parseLine(sc.Text()); ok {
			s.add(e)
		}
	}
	return sc.Err()
}

// Tail watches a log file for new lines in a background goroutine.
// Seeks to EOF first so already-loaded entries are not duplicated.
func (s *Store) Tail(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return err
	}
	go func() {
		defer f.Close()
		r := bufio.NewReader(f)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				time.Sleep(tailSleep)
				continue
			}
			if e, ok := parseLine(strings.TrimRight(line, "\n")); ok {
				s.add(e)
			}
		}
	}()
	return nil
}
