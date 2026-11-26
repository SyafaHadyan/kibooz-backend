package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/infra/validation"
	"github.com/kiboozapp/kibooz-backend/internal/response"
)

func (h *AuthHandler) GoogleLogin(c fiber.Ctx) error {
	var req dto.GoogleLoginRequest

	if err := validation.BindBody(c, &req); err != nil {
		return err
	}

	res, err := h.useCase.GoogleLogin(c.Context(), req)
	if err != nil {
		return err
	}

	message := "Login successful"
	if res.NeedsRegistration {
		message = "Google account verified, complete registration to continue"
	}

	return response.JSON(c, http.StatusOK, message, res)
}

func (h *AuthHandler) GoogleRegister(c fiber.Ctx) error {
	var req dto.GoogleRegisterRequest

	if err := validation.BindBody(c, &req); err != nil {
		return err
	}

	res, err := h.useCase.GoogleRegister(c.Context(), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Registration successful", res)
}
