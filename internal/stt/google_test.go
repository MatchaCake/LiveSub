package stt

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
)

func result(text string, final bool, lang string) *speechpb.StreamingRecognitionResult {
	return &speechpb.StreamingRecognitionResult{
		Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: text, Confidence: 0.9}},
		IsFinal:      final,
		LanguageCode: lang,
	}
}

func resp(rs ...*speechpb.StreamingRecognitionResult) *speechpb.StreamingRecognizeResponse {
	return &speechpb.StreamingRecognizeResponse{Results: rs}
}

// script replays canned responses and then returns err forever.
func script(err error, rs ...*speechpb.StreamingRecognizeResponse) func() (*speechpb.StreamingRecognizeResponse, error) {
	i := 0
	return func() (*speechpb.StreamingRecognizeResponse, error) {
		if i < len(rs) {
			i++
			return rs[i-1], nil
		}
		return nil, err
	}
}

func finals(ch chan StreamResult) []StreamResult {
	close(ch)
	var out []StreamResult
	for r := range ch {
		if r.IsFinal {
			out = append(out, r)
		}
	}
	return out
}

var errRotation = errors.New("rpc error: code = OutOfRange desc = Exceeded maximum allowed stream duration of 305 seconds.")

// 2026-11-11 21:06:28: the 305s cut-off landed mid-sentence and the first half
// of 「…の翻訳を翻訳しないことでした」 was lost with the interim results.
func TestRotationKeepsUnfinishedUtterance(t *testing.T) {
	ch := make(chan StreamResult, 16)
	err := receive(context.Background(), script(errRotation,
		resp(result("何が", false, "ja-jp")),
		resp(result(" 何が？", true, "ja-jp")),
		resp(result("固有", false, "ja-jp")),
		// stable part + unstable tail of the same utterance
		resp(result("固有名詞", false, "ja-jp"), result("の翻訳", false, "ja-jp")),
	), ch, "ja-jp")
	if err == nil || !strings.Contains(err.Error(), "Exceeded maximum allowed stream duration") {
		t.Fatalf("err = %v", err)
	}
	got := finals(ch)
	if len(got) != 2 || got[0].Text != " 何が？" || got[1].Text != "固有名詞の翻訳" {
		t.Fatalf("finals = %+v", got)
	}
	if got[1].Language != "ja-jp" {
		t.Errorf("lang = %q", got[1].Language)
	}
}

// A cut right after a final result has nothing pending: no duplicate line.
func TestRotationAfterFinalAddsNothing(t *testing.T) {
	ch := make(chan StreamResult, 16)
	_ = receive(context.Background(), script(errRotation,
		resp(result("こんばん", false, "ja-jp")),
		resp(result("こんばんは。", true, "ja-jp")),
		resp(), // event-only response must not resurrect anything
	), ch, "ja-jp")
	if got := finals(ch); len(got) != 1 || got[0].Text != "こんばんは。" {
		t.Fatalf("finals = %+v", got)
	}
}

// A final and the next utterance's interim can share one response.
func TestFinalFollowedByInterimInSameResponse(t *testing.T) {
	ch := make(chan StreamResult, 16)
	_ = receive(context.Background(), script(errRotation,
		resp(result("ありがとう。", true, "ja-jp"), result("それで", false, "")),
	), ch, "ja-jp")
	got := finals(ch)
	if len(got) != 2 || got[1].Text != "それで" {
		t.Fatalf("finals = %+v", got)
	}
	if got[1].Language != "ja-jp" {
		t.Errorf("missing language not defaulted: %q", got[1].Language)
	}
}

// Audio source ended (ffmpeg died): the server has already finalized
// everything, and nil tells the agent to restart the pipeline.
func TestAudioEOFReturnsNilWithoutFlush(t *testing.T) {
	ch := make(chan StreamResult, 16)
	err := receive(context.Background(), script(io.EOF, resp(result("途中", false, "ja-jp"))), ch, "ja-jp")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := finals(ch); len(got) != 0 {
		t.Fatalf("finals = %+v", got)
	}
}

// Stream going offline / service shutdown cancels the context: don't push a
// line into a pipeline that is being torn down.
func TestCancelledContextDoesNotFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan StreamResult, 16)
	err := receive(ctx, script(context.Canceled, resp(result("途中", false, "ja-jp"))), ch, "ja-jp")
	if err == nil {
		t.Fatal("want error")
	}
	if got := finals(ch); len(got) != 0 {
		t.Fatalf("finals = %+v", got)
	}
}
