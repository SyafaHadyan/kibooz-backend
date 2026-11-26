package dto

type DeleteAccountRequest struct {
	// Password confirms the deletion of an account that has one. An account that signs in with Google and has no password confirms
	// it with a fresh IDToken instead.
	Password string `json:"password" validate:"omitempty,max=72"`
	IDToken  string `json:"idToken" validate:"omitempty,max=4096"`
	// SuccessorEmail names the teacher who takes over the classes that no other teacher teaches. It is needed when the
	// account is the only teacher of a class and ignored otherwise.
	SuccessorEmail string `json:"successorEmail" validate:"omitempty,email,max=255"`
}

// ChangePasswordRequest swaps the password of the signed in account, after the current one is confirmed
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" validate:"required,max=72"`
	NewPassword     string `json:"newPassword" validate:"required,min=8,max=72,notcommon"`
}

// SetPasswordRequest gives an account that signs in with Google its first password. The fresh token of the linked Google account is
// the proof, because there is no password to check.
type SetPasswordRequest struct {
	IDToken     string `json:"idToken" validate:"required,max=4096"`
	NewPassword string `json:"newPassword" validate:"required,min=8,max=72,notcommon"`
}

// LinkGoogleRequest adds a Google account as a second way to sign in. The password and the fresh token are both needed, so a stolen
// access token alone cannot add a way in that outlives it.
type LinkGoogleRequest struct {
	IDToken  string `json:"idToken" validate:"required,max=4096"`
	Password string `json:"password" validate:"required,max=72"`
}

// UnlinkGoogleRequest removes the Google account from an account that has a password, after the password is confirmed
type UnlinkGoogleRequest struct {
	Password string `json:"password" validate:"required,max=72"`
}
