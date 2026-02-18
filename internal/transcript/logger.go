package transcript

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger writes timestamped source+translation pairs to CSV files.
// One file per stream per session (live start → live end).
type Logger struct {
	mu      sync.Mutex
	dir     string
	file    *os.File
	writer  *csv.Writer
	roomID  int64
	name    string
	session string // timestamp-based session ID
}

// NewLogger creates a transcript logger for a stream session.
// Files are saved as: <dir>/<room_id>_<name>_<date>_<time>.csv
func NewLogger(dir string, roomID int64, name string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}

	now := time.Now()
	session := now.Format("20060102_150405")
	safeName := sanitize(name)
	filename := fmt.Sprintf("%d_%s_%s.csv", roomID, safeName, session)
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create transcript file: %w", err)
	}

	// Write UTF-8 BOM for Excel compatibility
	f.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(f)
	w.Write([]string{"时间", "原文", "翻译"})
	w.Flush()

	return &Logger{
		dir:     dir,
		file:    f,
		writer:  w,
		roomID:  roomID,
		name:    name,
		session: session,
	}, nil
}

// Write logs a translation pair.
func (l *Logger) Write(source, translated string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer == nil {
		return
	}
	ts := time.Now().Format("15:04:05")
	l.writer.Write([]string{ts, source, translated})
	l.writer.Flush()
}

// Close flushes and closes the file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer != nil {
		l.writer.Flush()
	}
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Path returns the file path.
func (l *Logger) Path() string {
	if l.file == nil {
		return ""
	}
	return l.file.Name()
}

// sanitize makes a filename-safe string.
func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			out = append(out, '_')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

// ListFiles returns all transcript CSV files, newest first.
func ListFiles(dir string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []FileInfo
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	return files, nil
}

// ListFilesForRoom returns transcripts for a specific room.
func ListFilesForRoom(dir string, roomID int64) ([]FileInfo, error) {
	all, err := ListFiles(dir)
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%d_", roomID)
	var filtered []FileInfo
	for _, f := range all {
		if len(f.Name) > len(prefix) && f.Name[:len(prefix)] == prefix {
			filtered = append(filtered, f)
		}
	}
	return filtered, nil
}

// FileInfo describes a transcript file.
type FileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}
