package surface

import (
	"testing"

	"github.com/martcoca/work-tracker/packetexport"
)

// A superseded packet is retired work. The browse view is where a session or a reader picks
// what to do next, so advertising one as unclaimed sends them at a packet that has already
// been replaced — the hazard check-packet-index.sh prevents in the repository, reintroduced
// through the app. The detail view already carried superseded_by; the summary dropped it.
func TestSupersededPacketIsNotOfferedAsWork(t *testing.T) {
	replacement := "0004-E03-T03"
	superseded := summarize(packetexport.Record{
		ID: "0004-E03-T02", Status: "not started", SupersededBy: &replacement,
	})
	if superseded.Unclaimed {
		t.Error("a superseded packet is offered as unclaimed work")
	}
	if superseded.SupersededBy == nil || *superseded.SupersededBy != replacement {
		t.Error("the summary does not name the replacement")
	}

	// The narrow rule must not swallow ordinary available work.
	live := summarize(packetexport.Record{ID: "0004-E03-T01", Status: "not started"})
	if !live.Unclaimed {
		t.Error("an ordinary not-started packet must still be unclaimed")
	}
	if live.SupersededBy != nil {
		t.Error("a live packet must not name a replacement")
	}
}
