package audio

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type playURLResponse struct {
	Code int `json:"code"`
	Data struct {
		Durl []struct {
			URL string `json:"url"`
		} `json:"durl"`
	} `json:"data"`
}

// GetBilibiliStreamURL fetches the live stream FLV URL for a room.
func GetBilibiliStreamURL(roomID int64) (string, error) {
	url := fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/playUrl?cid=%d&quality=4&platform=web", roomID)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch play url: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	var result playURLResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if result.Code != 0 || len(result.Data.Durl) == 0 {
		return "", fmt.Errorf("no stream URL (code=%d)", result.Code)
	}

	return result.Data.Durl[0].URL, nil
}
