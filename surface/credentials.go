package surface

import (
	"errors"
	"net/http"
	"time"

	"github.com/martcoca/work-tracker/agentcredential"
	"github.com/martcoca/work-tracker/identity"
)

type createAgentCredentialRequest struct {
	PacketID  string `json:"packet_id"`
	AttemptID string `json:"attempt_id"`
	ExpiresAt string `json:"expires_at"`
}

type credentialMetadataResponse struct {
	Metadata agentcredential.Metadata `json:"metadata"`
}

type credentialListResponse struct {
	Credentials []agentcredential.Metadata `json:"credentials"`
}

func (service *Service) createAgentCredential(principal identity.Principal, request *http.Request) (any, error) {
	var body createAgentCredentialRequest
	if err := decodeAuthoringRequest(request, &body); err != nil {
		return nil, err
	}
	expiresAt, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		return nil, agentcredential.ErrInvalidCredential
	}
	now := service.now().UTC()
	if err := service.requireTenantPacket(principal, body.PacketID, now); err != nil {
		return nil, err
	}
	return service.credentials.Create(request.Context(), agentcredential.CreateCommand{
		TenantID: principal.TenantID, PacketID: body.PacketID, AttemptID: body.AttemptID,
		CreatedBy: principal.Subject, ExpiresAt: expiresAt,
	}, now)
}

func (service *Service) listAgentCredentials(principal identity.Principal, request *http.Request) (any, error) {
	credentials, err := service.credentials.List(request.Context(), principal.TenantID)
	if err != nil {
		return nil, err
	}
	return credentialListResponse{Credentials: credentials}, nil
}

func (service *Service) getAgentCredential(principal identity.Principal, request *http.Request) (any, error) {
	metadata, err := service.credentials.Get(request.Context(), principal.TenantID, request.PathValue("credential"))
	if err != nil {
		return nil, err
	}
	return credentialMetadataResponse{Metadata: metadata}, nil
}

func (service *Service) revokeAgentCredential(principal identity.Principal, request *http.Request) (any, error) {
	metadata, err := service.credentials.Revoke(
		request.Context(), principal.TenantID, request.PathValue("credential"), principal.Subject, service.now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return credentialMetadataResponse{Metadata: metadata}, nil
}

func (service *Service) requireTenantPacket(principal identity.Principal, packetID string, at time.Time) error {
	snapshot, err := service.currentSnapshot()
	if err != nil {
		return err
	}
	packets, _, err := snapshot.tenantPackets(principal, at)
	if err != nil {
		return err
	}
	for _, candidate := range packets {
		if candidate.record.ID == packetID {
			return nil
		}
	}
	return ErrViewNotFound
}

type agentOperation func(agentcredential.Identity, *http.Request) (any, error)

// agentAuthenticated is the machine-authentication seam E03-T01 will put in front of its
// authorized operations. Authentication contributes identity only; the operation must
// still obtain a live grant.
func (service *Service) agentAuthenticated(successStatus int, operation agentOperation) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if service.credentials == nil {
			service.writeOperationError(response, ErrCredentialStoreUnavailable)
			return
		}
		principal, err := service.credentials.Authenticate(
			request.Context(), request.Header.Get("Authorization"), service.now().UTC(),
		)
		if err != nil {
			writeAgentAuthenticationError(response, err)
			return
		}
		result, err := operation(principal, request)
		if err != nil {
			service.writeOperationError(response, err)
			return
		}
		writeJSON(response, successStatus, result)
	}
}

func writeAgentAuthenticationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentcredential.ErrMalformedHeader):
		writeAPIError(response, http.StatusUnauthorized, "malformed_authorization", "The agent authorization header is malformed.", ExportStatus{})
	case errors.Is(err, agentcredential.ErrUnknownCredential):
		writeAPIError(response, http.StatusUnauthorized, "unknown_credential", "The agent credential is unknown.", ExportStatus{})
	case errors.Is(err, agentcredential.ErrRevokedCredential):
		writeAPIError(response, http.StatusUnauthorized, "revoked_credential", "The agent credential was revoked.", ExportStatus{})
	case errors.Is(err, agentcredential.ErrExpiredCredential):
		writeAPIError(response, http.StatusUnauthorized, "expired_credential", "The agent credential expired.", ExportStatus{})
	default:
		writeAPIError(response, http.StatusInternalServerError, "authentication_failed", "Agent authentication could not be completed.", ExportStatus{})
	}
}
