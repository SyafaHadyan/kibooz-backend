package dto

type DeleteAccountRequest struct {
	Password string `json:"password" validate:"required,max=72"`
	// SuccessorEmail names the teacher who takes over the classes that no other teacher teaches. It is needed when the
	// account is the only teacher of a class and ignored otherwise.
	SuccessorEmail string `json:"successorEmail" validate:"omitempty,email,max=255"`
}

// ChangePasswordRequest swaps the password of the signed in account, after the current one is confirmed
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" validate:"required,max=72"`
	NewPassword     string `json:"newPassword" validate:"required,min=8,max=72,notcommon"`
}
