package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Capturer captures audio from a specific PipeWire node.
type Capturer struct {
	NodeID     int
	SampleRate int
	Channels   int
}

func NewCapturer(nodeID int) *Capturer {
	return &Capturer{
		NodeID:     nodeID,
		SampleRate: 16000,
		Channels:   1,
	}
}

// Start begins capturing audio and returns a reader of raw PCM s16le data.
func (c *Capturer) Start(ctx context.Context) (io.ReadCloser, error) {
	args := []string{
		"--target", strconv.Itoa(c.NodeID),
		"--format", "s16",
		"--rate", strconv.Itoa(c.SampleRate),
		"--channels", strconv.Itoa(c.Channels),
		"-",
	}

	cmd := exec.CommandContext(ctx, "pw-record", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pw-record: %w", err)
	}

	slog.Info("audio capture started", "node_id", c.NodeID)

	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		slog.Info("audio capture stopped", "node_id", c.NodeID)
	}()

	return stdout, nil
}

// ListSources lists available PipeWire audio source nodes.
func ListSources() error {
	cmd := exec.Command("pw-cli", "ls", "Node")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("pw-cli ls Node: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// BrowserSession manages a browser window + its PipeWire audio node.
type BrowserSession struct {
	RoomID  int64
	URL     string
	cmd     *exec.Cmd
	pid     int
	nodeID  int
}

// OpenBrowser launches a browser for the live room, waits for audio,
// and returns the PipeWire node ID.
func OpenBrowser(ctx context.Context, roomID int64) (*BrowserSession, error) {
	url := fmt.Sprintf("https://live.bilibili.com/%d", roomID)

	// Use chromium --app for a clean standalone window
	// Try chromium, then google-chrome, then firefox
	var browserCmd string
	for _, name := range []string{"chromium-browser", "chromium", "google-chrome", "google-chrome-stable"} {
		if path, err := exec.LookPath(name); err == nil {
			browserCmd = path
			break
		}
	}
	if browserCmd == "" {
		return nil, fmt.Errorf("no supported browser found (chromium/chrome)")
	}

	cmd := exec.CommandContext(ctx, browserCmd,
		"--app="+url,
		"--new-window",
		"--autoplay-policy=no-user-gesture-required", // auto-play live stream
		fmt.Sprintf("--user-data-dir=/tmp/livesub-chrome-%d", roomID), // isolated profile
	)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start browser: %w", err)
	}

	session := &BrowserSession{
		RoomID: roomID,
		URL:    url,
		cmd:    cmd,
		pid:    cmd.Process.Pid,
	}

	slog.Info("browser opened", "room", roomID, "pid", session.pid, "url", url)

	// Wait for PipeWire audio node to appear (browser needs time to start playing)
	nodeID, err := waitForAudioNode(ctx, session.pid, 30*time.Second)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("no audio node found: %w", err)
	}

	session.nodeID = nodeID
	slog.Info("audio node found", "room", roomID, "node_id", nodeID, "pid", session.pid)
	return session, nil
}

func (s *BrowserSession) NodeID() int {
	return s.nodeID
}

func (s *BrowserSession) Close() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		slog.Info("browser closed", "room", s.RoomID, "pid", s.pid)
	}
}

// waitForAudioNode polls PipeWire until a node matching the browser PID appears.
func waitForAudioNode(ctx context.Context, browserPID int, timeout time.Duration) (int, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	pidStr := strconv.Itoa(browserPID)

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline:
			return 0, fmt.Errorf("timeout waiting for audio node (pid=%d)", browserPID)
		case <-ticker.C:
			nodeID, err := findNodeByPID(pidStr)
			if err == nil && nodeID > 0 {
				return nodeID, nil
			}
		}
	}
}

// pwDumpNode represents a node from pw-dump output.
type pwDumpNode struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Info struct {
		Props map[string]interface{} `json:"props"`
	} `json:"info"`
}

// findNodeByPID searches pw-dump output for an audio stream node matching the given PID.
// Chromium spawns child processes, so we also check parent PIDs.
func findNodeByPID(targetPID string) (int, error) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return 0, fmt.Errorf("pw-dump: %w", err)
	}

	var nodes []pwDumpNode
	if err := json.Unmarshal(out, &nodes); err != nil {
		return 0, fmt.Errorf("parse pw-dump: %w", err)
	}

	// Also find all child PIDs of the browser
	childPIDs := getChildPIDs(targetPID)
	allPIDs := append([]string{targetPID}, childPIDs...)

	for _, node := range nodes {
		if node.Type != "PipeWire:Interface:Node" {
			continue
		}
		props := node.Info.Props
		if props == nil {
			continue
		}

		// Check media.class is audio playback
		mediaClass, _ := props["media.class"].(string)
		if mediaClass != "Stream/Output/Audio" {
			continue
		}

		// Check PID match
		nodePID := fmt.Sprintf("%v", props["application.process.id"])
		for _, pid := range allPIDs {
			if nodePID == pid {
				return node.ID, nil
			}
		}
	}

	return 0, fmt.Errorf("no audio node for pid %s", targetPID)
}

// getChildPIDs returns all descendant PIDs of a process.
func getChildPIDs(parentPID string) []string {
	out, err := exec.Command("pgrep", "-P", parentPID).Output()
	if err != nil {
		return nil
	}

	var pids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			pids = append(pids, line)
			// Recurse for grandchildren (Chromium has deep process tree)
			pids = append(pids, getChildPIDs(line)...)
		}
	}
	return pids
}
