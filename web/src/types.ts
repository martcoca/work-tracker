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

export interface DraftView {
  id: string;
  packet_id: string;
  initiative_id: string;
  epic_id: string;
  target: string;
  tenant_id: string;
  parent_id?: string;
  state: "draft" | "issued";
  version: number;
  goal: string;
  boundary: string;
  done_when: string;
  check: string;
  context: string;
}

export interface DraftResponse {
  draft: DraftView;
}

export interface IssuedResponse {
  draft: DraftView;
  packet: PacketRecord;
  parent?: PacketRecord;
}

export interface AgentWorkload {
  kind: "workload";
  issuer: string;
  subject: string;
}

export interface AgentCredentialBinding {
  packet_id: string;
  attempt_id: string;
  issued_at: string;
  expires_at: string;
}

export interface AgentCredentialMetadata {
  id: string;
  tenant_id: string;
  workload: AgentWorkload;
  binding: AgentCredentialBinding;
  created_by: string;
  created_at: string;
  revoked_by?: string;
  revoked_at?: string;
  last_used_at?: string;
}

export interface AgentCredentialListResponse {
  credentials: AgentCredentialMetadata[];
}

export interface AgentCredentialIssuedResponse {
  credential: string;
  metadata: AgentCredentialMetadata;
}

export interface AgentCredentialMetadataResponse {
  metadata: AgentCredentialMetadata;
}

export interface APIErrorBody {
  code: string;
  message: string;
  directory?: DirectoryStatus;
}
