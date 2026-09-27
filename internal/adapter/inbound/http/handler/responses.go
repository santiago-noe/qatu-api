package handler

import "github.com/santiago-noe/qatu-api/internal/core/domain"

// userResponse es la vista pública del usuario (sin datos sensibles).
type userResponse struct {
	ID                string        `json:"id"`
	Email             string        `json:"email,omitempty"`
	EmailVerified     bool          `json:"email_verified"`
	Name              string        `json:"name"`
	AvatarURL         string        `json:"avatar_url,omitempty"`
	Roles             []domain.Role `json:"roles"`
	Status            string        `json:"status"`
	VerificationLevel int           `json:"verification_level"`
	CanTransact       bool          `json:"can_transact"`
}

func toUserResponse(u domain.User) userResponse {
	return userResponse{
		ID: u.ID, Email: u.Email, EmailVerified: u.EmailVerifiedAt != nil, Name: u.Name, AvatarURL: u.AvatarURL,
		Roles: u.Roles, Status: string(u.Status), VerificationLevel: u.VerificationLevel, CanTransact: u.CanTransact(),
	}
}
