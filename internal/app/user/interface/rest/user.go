// Package rest exposes the profile endpoints over HTTP
package rest

import (
	"io"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type UserHandler struct {
	useCase usecase.UserUseCaseItf
}

func NewUserHandler(router fiber.Router, authLimiter fiber.Handler, mw middleware.MiddlewareItf, useCase usecase.UserUseCaseItf) {
	handler := UserHandler{useCase: useCase}

	router.Post("/users/avatar", mw.Authentication, mw.RequireRole(constants.RoleGuru, constants.RoleWali), handler.UploadAvatar)
	// the password is checked here, so the same strict limit as the login route applies
	router.Delete("/users/me", authLimiter, mw.Authentication, mw.RequireRole(constants.RoleGuru, constants.RoleWali), handler.DeleteAccount)
}

func (h *UserHandler) DeleteAccount(c fiber.Ctx) error {
	var req dto.DeleteAccountRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	err = h.useCase.DeleteAccount(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), req.Password)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Account deleted", nil)
}

func (h *UserHandler) UploadAvatar(c fiber.Ctx) error {
	var studentID *uuid.UUID

	if raw := c.FormValue("studentId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return apperror.Validation(map[string]string{"studentId": "format UUID tidak valid"})
		}

		studentID = &parsed
	}

	header, err := c.FormFile("file")
	if err != nil {
		return apperror.Validation(map[string]string{"file": "wajib diisi"})
	}

	if header.Size > constants.AvatarMaxBytes {
		return apperror.ErrFileTooLarge
	}

	file, err := header.Open()
	if err != nil {
		return apperror.Internal(err)
	}

	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, constants.AvatarMaxBytes+1))
	if err != nil {
		return apperror.Internal(err)
	}

	res, err := h.useCase.UploadAvatar(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), studentID, data)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Foto profil berhasil diperbarui", res)
}
