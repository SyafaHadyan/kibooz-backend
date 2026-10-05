// Package rest exposes the auth usecase over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type AuthHandler struct {
	useCase usecase.AuthUseCaseItf
}

func NewAuthHandler(router fiber.Router, authLimiter fiber.Handler, useCase usecase.AuthUseCaseItf) {
	handler := AuthHandler{useCase: useCase}

	group := router.Group("/auth", authLimiter)

	group.Post("/register", handler.Register)
	group.Post("/login", handler.Login)
	group.Post("/refresh-token", handler.Refresh)
	group.Post("/logout", handler.Logout)
}

func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req dto.RegisterRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.Register(c.Context(), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Registrasi berhasil", res)
}

func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req dto.LoginRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.Login(c.Context(), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Login berhasil", res)
}

func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req dto.RefreshRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.Refresh(c.Context(), req.RefreshToken)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Token berhasil diperbarui", res)
}

func (h *AuthHandler) Logout(c fiber.Ctx) error {
	var req dto.LogoutRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	err = h.useCase.Logout(c.Context(), req.RefreshToken)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Logout berhasil", nil)
}
