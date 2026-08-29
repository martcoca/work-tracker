export interface DirectoryStatus {
  published_at: string;
  expires_at: string;
  age_seconds: number;
  stale: boolean;
  expired_by_seconds?: number;
}

export interface InitiativeSummary {
  id: string;
  epic_count: number;
  packet_count: number;
  blocked_count: number;
  unclaimed_count: number;
}

export interface EpicSummary {
  id: string;
  packet_count: number;
  blocked_count: number;
  unclaimed_count: number;
}

export interface PacketSummary {
  id: string;
  status: string;
  taken_by: string | null;
  blocked: boolean;
  unclaimed: boolean;
}

export interface Comment {
  event_id: string;
  timestamp: string;
  actor: string;
  text: string;
}

export interface HistoryEvent {
  kind: string;
  event_id: string;
  timestamp: string;
  actor: string;
  tenant_id?: string;
  body?: {
    goal: string;
    boundary: string;
    done_when: string;
    check: string;
    context: string;
  };
  parent_id?: string;
  text?: string;
  from?: string;
  to?: string;
  evidence?: string[];
  replacement_id?: string;
  reason?: string;
}

export interface PacketRecord {
  id: string;
  tenant_id: string;
  goal: string;
  boundary: string;
  done_when: string;
  check: string;
  context: string;
  status: string;
  version: number;
  taken_by: string | null;
  comments: Comment[];
  evidence: string[];
  parent_id: string | null;
  superseded_by: string | null;
  closure?: {
    event_id: string;
    timestamp: string;
    actor: string;
    reason: string;
  } | null;
  history: HistoryEvent[];
}

export interface InitiativesView {
  directory: DirectoryStatus;
  initiatives: InitiativeSummary[];
}

export interface InitiativeView {
  directory: DirectoryStatus;
  id: string;
  epics: EpicSummary[];
}

export interface EpicView {
  directory: DirectoryStatus;
  initiative_id: string;
  id: string;
  packets: PacketSummary[];
}

export interface PacketView {
  directory: DirectoryStatus;
  packet: PacketRecord;
}

export interface APIErrorBody {
  code: string;
  message: string;
  directory?: DirectoryStatus;
}
