package dto

type DeleteAccountRequest struct {
	Password string `json:"password" validate:"required,max=72"`
}
