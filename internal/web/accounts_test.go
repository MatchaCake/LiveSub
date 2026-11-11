package web

import (
	"context"
	"strings"
	"testing"

	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/config"
)

type stubBot struct {
	name  string
	avail bool
}

func (b stubBot) Send(context.Context, int64, string) error { return nil }
func (b stubBot) Platform() string                          { return "bilibili" }
func (b stubBot) Name() string                              { return b.name }
func (b stubBot) Available() bool                           { return b.avail }
func (b stubBot) MaxMessageLen() int                        { return 30 }

// The panel must flag dead outputs while the streamer is offline too — that is
// when someone has time to re-scan, not twenty minutes into the stream.
func TestOfflineOutputsReportAccountHealth(t *testing.T) {
	pool := bot.NewPool()
	pool.Add(stubBot{name: "live", avail: true})
	pool.Add(stubBot{name: "blanked", avail: false}) // cookie expired
	cfg := &config.Config{Streamers: []config.StreamerConfig{{
		Name: "streamer", RoomID: 1001,
		Outputs: []config.OutputConfig{
			{Name: "jp", Accounts: []string{"missing"}},
			{Name: "jp2", Accounts: []string{"live"}},
			{Name: "jp4", Accounts: []string{"blanked", "live", "missing"}},
		},
	}}}
	s := &Server{pool: pool, cfg: cfg, streamers: map[string]*streamerRuntime{}}

	states := s.streamerStates(nil)
	if len(states) != 1 || len(states[0].Outputs) != 3 {
		t.Fatalf("states = %+v", states)
	}
	jp, jp2, jp4 := states[0].Outputs[0], states[0].Outputs[1], states[0].Outputs[2]
	if !jp.NoAccounts || strings.Join(jp.DeadBots, ",") != "missing" {
		t.Errorf("jp: %+v", jp)
	}
	if jp2.NoAccounts || len(jp2.DeadBots) != 0 {
		t.Errorf("jp2: %+v", jp2)
	}
	if jp4.NoAccounts || strings.Join(jp4.DeadBots, ",") != "blanked,missing" || strings.Join(jp4.UsableBots, ",") != "live" {
		t.Errorf("jp4: %+v", jp4)
	}
}
