package auth

type StreamInfo struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	RoomID     int64  `json:"room_id"`
	SourceLang string `json:"source_lang"`
	TargetLang string `json:"target_lang"`
	CreatedAt  string `json:"created_at"`
}

func (s *Store) migrateStreams() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS streams (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			room_id INTEGER UNIQUE NOT NULL,
			source_lang TEXT NOT NULL DEFAULT 'ja-JP',
			target_lang TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS hidden_streams (
			room_id INTEGER PRIMARY KEY
		);
	`)
	return err
}

// HideStream hides a config-based stream (won't affect DB streams, use DeleteStream for those).
func (s *Store) HideStream(roomID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO hidden_streams (room_id) VALUES (?)`, roomID)
	return err
}

// ListHiddenRooms returns all hidden room IDs.
func (s *Store) ListHiddenRooms() map[int64]bool {
	m := make(map[int64]bool)
	rows, err := s.db.Query(`SELECT room_id FROM hidden_streams`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		m[id] = true
	}
	return m
}

func (s *Store) AddStream(name string, roomID int64, sourceLang string) (*StreamInfo, error) {
	if sourceLang == "" {
		sourceLang = "ja-JP"
	}
	res, err := s.db.Exec(
		`INSERT INTO streams (name, room_id, source_lang) VALUES (?, ?, ?)`,
		name, roomID, sourceLang,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &StreamInfo{ID: id, Name: name, RoomID: roomID, SourceLang: sourceLang}, nil
}

func (s *Store) DeleteStream(id int64) error {
	_, err := s.db.Exec(`DELETE FROM streams WHERE id = ?`, id)
	return err
}

func (s *Store) ListStreams() ([]StreamInfo, error) {
	rows, err := s.db.Query(`SELECT id, name, room_id, source_lang, COALESCE(target_lang,''), created_at FROM streams ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []StreamInfo
	for rows.Next() {
		var si StreamInfo
		if err := rows.Scan(&si.ID, &si.Name, &si.RoomID, &si.SourceLang, &si.TargetLang, &si.CreatedAt); err != nil {
			return nil, err
		}
		streams = append(streams, si)
	}
	return streams, nil
}
