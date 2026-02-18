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
	`)
	return err
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
		rows.Scan(&si.ID, &si.Name, &si.RoomID, &si.SourceLang, &si.TargetLang, &si.CreatedAt)
		streams = append(streams, si)
	}
	return streams, nil
}
