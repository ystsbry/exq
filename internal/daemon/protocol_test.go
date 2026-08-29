package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJobStateDone(t *testing.T) {
	tests := []struct {
		state JobState
		want  bool
	}{
		{JobQueued, false},
		{JobRunning, false},
		{JobSucceeded, true},
		{JobFailed, true},
		{JobStopped, true},
		{JobState("bogus"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.Done(); got != tt.want {
				t.Errorf("%q.Done() = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestJobInfoOmitsUnsetTimestamps(t *testing.T) {
	// A queued job has never started or finished; those zero times must not
	// reach the wire as "0001-01-01T00:00:00Z".
	data, err := json.Marshal(JobInfo{
		ID:        "j1",
		State:     JobQueued,
		CreatedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"started_at", "finished_at", "reason"} {
		if strings.Contains(string(data), field) {
			t.Errorf("unset %s should be omitted:\n%s", field, data)
		}
	}
	if !strings.Contains(string(data), "created_at") {
		t.Errorf("created_at should always be present:\n%s", data)
	}
}

func TestRequestOmitsUnusedFields(t *testing.T) {
	// A ping carries no job spec and no job id.
	data, err := json.Marshal(Request{Version: ProtocolVersion, Op: OpPing})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"version":1,"op":"ping"}`; got != want {
		t.Errorf("ping request = %s, want %s", got, want)
	}
}
