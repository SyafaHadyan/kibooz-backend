package dto

type DeleteAccountRequest struct {
	Password string `json:"password" validate:"required,max=72"`
	// SuccessorEmail names the teacher who takes over the classes that no other teacher teaches. It is needed when the
	// account is the only teacher of a class and ignored otherwise.
	SuccessorEmail string `json:"successorEmail" validate:"omitempty,email,max=255"`
}
