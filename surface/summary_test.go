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

// The epic and initiative cards count through countWaiting, not summarize. Fixing only
// the row left the cards advertising retired work: Epic E03 read "3 unclaimed" over three
// packets of which one was superseded. Both paths now share isUnclaimed, and this covers
// the rollup directly so they cannot drift apart again.
func TestRollupCountsExcludeSupersededPackets(t *testing.T) {
	replacement := "0004-E03-T03"
	records := []packetexport.Record{
		{ID: "0004-E03-T01", Status: "not started"},
		{ID: "0004-E03-T02", Status: "not started", SupersededBy: &replacement},
		{ID: "0004-E03-T03", Status: "not started"},
	}
	blocked, unclaimed := 0, 0
	for _, record := range records {
		countWaiting(&blocked, &unclaimed, record)
	}
	if unclaimed != 2 {
		t.Errorf("unclaimed = %d, want 2: the superseded packet is still counted as work", unclaimed)
	}
	if blocked != 0 {
		t.Errorf("blocked = %d, want 0", blocked)
	}

	// The row and the card must agree, because a reader takes them as one claim.
	for _, record := range records {
		if summarize(record).Unclaimed != isUnclaimed(record) {
			t.Errorf("%s: summary and rollup disagree", record.ID)
		}
	}
}
