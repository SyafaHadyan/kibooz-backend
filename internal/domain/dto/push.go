package dto

// PushTokenRequest is the token that Firebase gave the installed app. Platform is android when it is left out. The length is
// checked after the trim by the use case, so it is not part of the tags here.
type PushTokenRequest struct {
	Token    string `json:"token" validate:"required"`
	Platform string `json:"platform" validate:"omitempty,oneof=android"`
}

// RemovePushTokenRequest names the token of the phone that signs out
type RemovePushTokenRequest struct {
	Token string `json:"token" validate:"required"`
}
