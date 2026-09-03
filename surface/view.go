package surface

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/martcoca/work-tracker/contract"
	"github.com/martcoca/work-tracker/identity"
	"github.com/martcoca/work-tracker/packetexport"
	"github.com/martcoca/work-tracker/tenant"
)

var (
	ErrDirectoryStale    = errors.New("tenant directory is stale")
	ErrPacketExportStale = errors.New("packet export is stale")
	ErrTenantIsolation   = errors.New("requested work belongs to another tenant")
	ErrViewNotFound      = errors.New("requested view not found")
	packetIDPattern      = regexp.MustCompile(`^([0-9]{4})-(E[0-9]{2})-(T[0-9]{2})$`)
)

// ExportStatus is safe status metadata; it never contains tenant or packet contents.
type ExportStatus struct {
	PublishedAt string `json:"published_at"`
	ExpiresAt   string `json:"expires_at"`
	AgeSeconds  int64  `json:"age_seconds"`
	Stale       bool   `json:"stale"`
	ExpiredBy   int64  `json:"expired_by_seconds,omitempty"`
}

// HeldExportStatus reports freshness and refresh facts without exposing an export's
// payload. It is safe to return from the public health route.
type HeldExportStatus struct {
	Name         string `json:"name"`
	Available    bool   `json:"available"`
	Required     bool   `json:"required"`
	ServiceOwned bool   `json:"service_owned"`
	Absent       bool   `json:"absent"`
	PublishedAt  string `json:"published_at,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	AgeSeconds   int64  `json:"age_seconds,omitempty"`
	Stale        bool   `json:"stale"`
	ExpiredBy    int64  `json:"expired_by_seconds,omitempty"`
	LastAttempt  string `json:"last_attempt,omitempty"`
	LastSuccess  string `json:"last_success,omitempty"`
	RefreshError string `json:"refresh_error,omitempty"`
}

type PacketSummary struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// SupersededBy names the replacement when this packet has been retired. Supersession
	// is not a Status — a packet keeps the status it had when it was replaced — so without
	// this field a retired packet is indistinguishable here from available work.
	SupersededBy *string `json:"superseded_by"`
	TakenBy      *string `json:"taken_by"`
	Blocked      bool    `json:"blocked"`
	Unclaimed    bool    `json:"unclaimed"`
}

type EpicSummary struct {
	ID             string `json:"id"`
	PacketCount    int    `json:"packet_count"`
	BlockedCount   int    `json:"blocked_count"`
	UnclaimedCount int    `json:"unclaimed_count"`
}

type InitiativeSummary struct {
	ID             string `json:"id"`
	EpicCount      int    `json:"epic_count"`
	PacketCount    int    `json:"packet_count"`
	BlockedCount   int    `json:"blocked_count"`
	UnclaimedCount int    `json:"unclaimed_count"`
}

type InitiativesView struct {
	Directory   ExportStatus        `json:"directory"`
	Initiatives []InitiativeSummary `json:"initiatives"`
}

type InitiativeView struct {
	Directory ExportStatus  `json:"directory"`
	ID        string        `json:"id"`
	Epics     []EpicSummary `json:"epics"`
}

type EpicView struct {
	Directory  ExportStatus    `json:"directory"`
	Initiative string          `json:"initiative_id"`
	ID         string          `json:"id"`
	Packets    []PacketSummary `json:"packets"`
}

type PacketView struct {
	Directory ExportStatus        `json:"directory"`
	Packet    packetexport.Record `json:"packet"`
}

type indexedPacket struct {
	record     packetexport.Record
	initiative string
	epic       string
}

// Snapshot is the last verified pair of exports held by the product. Stale snapshots
// remain inspectable for status reporting, but cannot authorize or return packet data.
type Snapshot struct {
	directory          *tenant.Directory
	directoryPublished time.Time
	directoryExpires   time.Time
	packetAvailable    bool
	packetPublished    time.Time
	packetExpires      time.Time
	packets            []indexedPacket
}

// NewSnapshot verifies both stored exports without requiring either publisher to be
// reachable. Freshness is enforced later against the time of each render request.
func NewSnapshot(packetContents, directoryContents []byte) (*Snapshot, error) {
	packetAt, err := inspectionTime(packetContents)
	if err != nil {
		return nil, fmt.Errorf("inspect packet export: %w", err)
	}
	verifiedPackets, err := packetexport.Verify(packetContents, packetAt)
	if err != nil {
		return nil, err
	}
	snapshot, err := NewEmptySnapshot(directoryContents)
	if err != nil {
		return nil, err
	}
	packetPublished, packetExpires, err := envelopeTimes(verifiedPackets.Envelope)
	if err != nil {
		return nil, err
	}
	snapshot.packetAvailable = true
	snapshot.packetPublished = packetPublished
	snapshot.packetExpires = packetExpires
	snapshot.packets = make([]indexedPacket, 0, len(verifiedPackets.Packets))
	for _, record := range verifiedPackets.Packets {
		matches := packetIDPattern.FindStringSubmatch(record.ID)
		if matches == nil {
			return nil, fmt.Errorf("%w: packet id %q does not name initiative, epic, and task", contract.ErrInvalidExport, record.ID)
		}
		indexed := indexedPacket{record: record, initiative: matches[1], epic: matches[2]}
		snapshot.packets = append(snapshot.packets, indexed)
	}
	return snapshot, nil
}

// NewEmptySnapshot creates the explicit first-run state: authority is verified, but this
// tracker has not published its service-owned packet export yet.
func NewEmptySnapshot(directoryContents []byte) (*Snapshot, error) {
	directoryAt, err := inspectionTime(directoryContents)
	if err != nil {
		return nil, fmt.Errorf("inspect tenant directory: %w", err)
	}
	directoryEnvelope, err := contract.Verify(directoryContents, tenant.Schema, directoryAt)
	if err != nil {
		return nil, err
	}
	directory, err := tenant.Parse(directoryContents, directoryAt)
	if err != nil {
		return nil, err
	}
	directoryPublished, directoryExpires, err := envelopeTimes(directoryEnvelope)
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		directory: directory, directoryPublished: directoryPublished, directoryExpires: directoryExpires,
		packets: []indexedPacket{},
	}, nil
}

func inspectionTime(contents []byte) (time.Time, error) {
	var timing struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(contents, &timing); err != nil {
		return time.Time{}, fmt.Errorf("%w: decode expiration", contract.ErrInvalidExport)
	}
	expiresAt, err := time.Parse(time.RFC3339, timing.ExpiresAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: expires_at must be RFC 3339", contract.ErrInvalidExport)
	}
	return expiresAt.Add(-time.Nanosecond), nil
}

func envelopeTimes(envelope contract.Envelope) (time.Time, time.Time, error) {
	publishedAt, err := time.Parse(time.RFC3339, envelope.PublishedAt)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339, envelope.ExpiresAt)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return publishedAt, expiresAt, nil
}

func (snapshot *Snapshot) directoryStatus(now time.Time) ExportStatus {
	return makeStatus(snapshot.directoryPublished, snapshot.directoryExpires, now)
}

func (snapshot *Snapshot) heldStatuses(now time.Time) []HeldExportStatus {
	statuses := []HeldExportStatus{
		{Name: "packets", Required: false, ServiceOwned: true, Absent: !snapshot.packetAvailable},
		heldStatus("tenant-directory", snapshot.directoryPublished, snapshot.directoryExpires, now, true, false),
	}
	if snapshot.packetAvailable {
		statuses[0] = heldStatus("packets", snapshot.packetPublished, snapshot.packetExpires, now, false, true)
	}
	return statuses
}

func heldStatus(name string, publishedAt, expiresAt, now time.Time, required, serviceOwned bool) HeldExportStatus {
	status := makeStatus(publishedAt, expiresAt, now)
	return HeldExportStatus{
		Name: name, Available: true, Required: required, ServiceOwned: serviceOwned,
		PublishedAt: status.PublishedAt, ExpiresAt: status.ExpiresAt,
		AgeSeconds: status.AgeSeconds, Stale: status.Stale, ExpiredBy: status.ExpiredBy,
	}
}

func makeStatus(publishedAt, expiresAt, now time.Time) ExportStatus {
	age := now.Sub(publishedAt)
	if age < 0 {
		age = 0
	}
	status := ExportStatus{
		PublishedAt: publishedAt.Format(time.RFC3339Nano), ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		AgeSeconds: int64(age / time.Second), Stale: !now.Before(expiresAt),
	}
	if status.Stale {
		status.ExpiredBy = int64(now.Sub(expiresAt) / time.Second)
	}
	return status
}

func (snapshot *Snapshot) tenantPackets(principal identity.Principal, now time.Time) ([]indexedPacket, ExportStatus, error) {
	status := snapshot.directoryStatus(now)
	if status.Stale {
		return nil, status, ErrDirectoryStale
	}
	if err := snapshot.directory.ValidateTenantID(principal.TenantID, now); err != nil {
		return nil, status, err
	}
	if snapshot.packetAvailable && !now.Before(snapshot.packetExpires) {
		return nil, status, ErrPacketExportStale
	}
	packets := make([]indexedPacket, 0)
	for _, indexed := range snapshot.packets {
		if indexed.record.TenantID == principal.TenantID {
			packets = append(packets, indexed)
		}
	}
	return packets, status, nil
}

func (snapshot *Snapshot) Initiatives(principal identity.Principal, now time.Time) (InitiativesView, error) {
	packets, status, err := snapshot.tenantPackets(principal, now)
	if err != nil {
		return InitiativesView{Directory: status}, err
	}
	byID := make(map[string]*InitiativeSummary)
	epics := make(map[string]map[string]struct{})
	for _, indexed := range packets {
		summary := byID[indexed.initiative]
		if summary == nil {
			summary = &InitiativeSummary{ID: indexed.initiative}
			byID[indexed.initiative] = summary
			epics[indexed.initiative] = make(map[string]struct{})
		}
		summary.PacketCount++
		epics[indexed.initiative][indexed.epic] = struct{}{}
		countWaiting(&summary.BlockedCount, &summary.UnclaimedCount, indexed.record)
	}
	initiatives := make([]InitiativeSummary, 0, len(byID))
	for id, summary := range byID {
		summary.EpicCount = len(epics[id])
		initiatives = append(initiatives, *summary)
	}
	sort.Slice(initiatives, func(left, right int) bool { return initiatives[left].ID < initiatives[right].ID })
	return InitiativesView{Directory: status, Initiatives: initiatives}, nil
}

func (snapshot *Snapshot) Initiative(principal identity.Principal, initiativeID string, now time.Time) (InitiativeView, error) {
	packets, status, err := snapshot.tenantPackets(principal, now)
	if err != nil {
		return InitiativeView{Directory: status}, err
	}
	byID := make(map[string]*EpicSummary)
	for _, indexed := range packets {
		if indexed.initiative != initiativeID {
			continue
		}
		summary := byID[indexed.epic]
		if summary == nil {
			summary = &EpicSummary{ID: indexed.epic}
			byID[indexed.epic] = summary
		}
		summary.PacketCount++
		countWaiting(&summary.BlockedCount, &summary.UnclaimedCount, indexed.record)
	}
	if len(byID) == 0 {
		return InitiativeView{Directory: status}, snapshot.notFoundOrIsolated(principal.TenantID, initiativeID, "", "")
	}
	epics := make([]EpicSummary, 0, len(byID))
	for _, summary := range byID {
		epics = append(epics, *summary)
	}
	sort.Slice(epics, func(left, right int) bool { return epics[left].ID < epics[right].ID })
	return InitiativeView{Directory: status, ID: initiativeID, Epics: epics}, nil
}

func (snapshot *Snapshot) Epic(principal identity.Principal, initiativeID, epicID string, now time.Time) (EpicView, error) {
	packets, status, err := snapshot.tenantPackets(principal, now)
	if err != nil {
		return EpicView{Directory: status}, err
	}
	summaries := make([]PacketSummary, 0)
	for _, indexed := range packets {
		if indexed.initiative == initiativeID && indexed.epic == epicID {
			summaries = append(summaries, summarize(indexed.record))
		}
	}
	if len(summaries) == 0 {
		return EpicView{Directory: status}, snapshot.notFoundOrIsolated(principal.TenantID, initiativeID, epicID, "")
	}
	sort.Slice(summaries, func(left, right int) bool { return summaries[left].ID < summaries[right].ID })
	return EpicView{Directory: status, Initiative: initiativeID, ID: epicID, Packets: summaries}, nil
}

func (snapshot *Snapshot) Packet(principal identity.Principal, initiativeID, epicID, packetID string, now time.Time) (PacketView, error) {
	packets, status, err := snapshot.tenantPackets(principal, now)
	if err != nil {
		return PacketView{Directory: status}, err
	}
	for _, indexed := range packets {
		if indexed.record.ID == packetID && indexed.initiative == initiativeID && indexed.epic == epicID {
			return PacketView{Directory: status, Packet: indexed.record}, nil
		}
	}
	return PacketView{Directory: status}, snapshot.notFoundOrIsolated(principal.TenantID, initiativeID, epicID, packetID)
}

func (snapshot *Snapshot) notFoundOrIsolated(tenantID, initiativeID, epicID, packetID string) error {
	for _, indexed := range snapshot.packets {
		matches := indexed.initiative == initiativeID
		matches = matches && (epicID == "" || indexed.epic == epicID)
		matches = matches && (packetID == "" || indexed.record.ID == packetID)
		if matches && indexed.record.TenantID != tenantID {
			return ErrTenantIsolation
		}
	}
	return ErrViewNotFound
}

// isUnclaimed is the single definition of "available to take". The rollup counts on the
// initiative and epic cards and the per-packet summary must agree, because they are read
// as the same claim about the same packet; when only one of them knew about supersession
// the cards advertised retired work while the row beneath them did not.
func isUnclaimed(record packetexport.Record) bool {
	superseded := record.SupersededBy != nil && *record.SupersededBy != ""
	return record.Status == "not started" && record.TakenBy == nil && !superseded
}

func countWaiting(blocked, unclaimed *int, record packetexport.Record) {
	if record.Status == "blocked" {
		(*blocked)++
	}
	if isUnclaimed(record) {
		(*unclaimed)++
	}
}

func summarize(record packetexport.Record) PacketSummary {
	// A superseded packet keeps whatever status it held when it was replaced, usually
	// "not started". Offering it as unclaimed sends a reader at work that no longer
	// exists, so supersession disqualifies it regardless of status.
	return PacketSummary{
		ID: record.ID, Status: record.Status,
		SupersededBy: record.SupersededBy, TakenBy: record.TakenBy,
		Blocked:   record.Status == "blocked",
		Unclaimed: isUnclaimed(record),
	}
}
