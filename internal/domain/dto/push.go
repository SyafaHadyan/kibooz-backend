package dto

// PushTokenRequest is the token that Firebase gave the installed app. Platform is android when it is left out.
type PushTokenRequest struct {
	Token    string `json:"token" validate:"required,min=20,max=4096"`
	Platform string `json:"platform" validate:"omitempty,oneof=android"`
}

// RemovePushTokenRequest names the token of the phone that signs out
type RemovePushTokenRequest struct {
	Token string `json:"token" validate:"required,min=20,max=4096"`
}
