// Package rest exposes the teacher portal over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type GuruHandler struct {
	useCase usecase.GuruUseCaseItf
}

// batchLimit limits the batch mood route per user and has to run after authentication
func NewGuruHandler(router fiber.Router, mw middleware.MiddlewareItf, batchLimit fiber.Handler, useCase usecase.GuruUseCaseItf) {
	handler := GuruHandler{useCase: useCase}

	group := router.Group("/guru", mw.Authentication, mw.RequireRole(constants.RoleGuru))
	idempotent := mw.Idempotent()

	group.Get("/dashboard", handler.Dashboard)
	group.Post("/mood/log", idempotent, handler.LogMood)
	group.Post("/mood/log/batch", batchLimit, idempotent, handler.LogMoodBatch)
	group.Get("/mood/analytics", handler.MoodAnalytics)
	group.Get("/classes", handler.ListClasses)
	group.Post("/classes", idempotent, handler.CreateClass)
	group.Get("/classes/:classId", handler.Class)
	group.Post("/classes/:classId/join-code", handler.RotateJoinCode)
	group.Put("/classes/:classId/joining", handler.SetJoining)
	group.Delete("/classes/:classId/students/:studentId", handler.RemoveStudent)
	group.Get("/classes/:classId/students/:studentId/history", handler.StudentHistory)
	group.Get("/classes/:classId/move-requests", handler.MoveRequests)
	group.Post("/classes/:classId/move-requests/:requestId/accept", handler.AcceptMove)
	group.Post("/classes/:classId/move-requests/:requestId/reject", handler.RejectMove)
	group.Get("/classes/:classId/teachers", handler.Teachers)
	group.Post("/classes/:classId/teachers", idempotent, handler.AddTeacher)
	group.Delete("/classes/:classId/teachers/:teacherId", handler.RemoveTeacher)
	group.Put("/classes/:classId/owner", handler.TransferOwnership)
	group.Get("/profile", handler.Profile)
	group.Put("/profile", handler.UpdateProfile)
	group.Get("/profile/detail", handler.ProfileDetail)
}

func (h *GuruHandler) Dashboard(c fiber.Ctx) error {
	classID, err := optionalUUID(c, "classId")
	if err != nil {
		return err
	}

	res, err := h.useCase.Dashboard(c.Context(), middleware.UserIDFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) LogMood(c fiber.Ctx) error {
	var req dto.LogMoodRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.LogMood(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Student mood log saved", res)
}

func (h *GuruHandler) LogMoodBatch(c fiber.Ctx) error {
	var req dto.LogMoodBatchRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.LogMoodBatch(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Mood logs saved", res)
}

func (h *GuruHandler) MoodAnalytics(c fiber.Ctx) error {
	classID, err := optionalUUID(c, "classId")
	if err != nil {
		return err
	}

	res, err := h.useCase.MoodAnalytics(c.Context(), middleware.UserIDFrom(c), classID, c.Query("range"))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) ListClasses(c fiber.Ctx) error {
	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListClasses(c.Context(), middleware.UserIDFrom(c), page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) Class(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	res, err := h.useCase.Class(c.Context(), middleware.UserIDFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) RotateJoinCode(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	res, err := h.useCase.RotateJoinCode(c.Context(), middleware.UserIDFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Join code replaced", res)
}

func (h *GuruHandler) SetJoining(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	var req dto.SetJoiningRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.SetJoining(c.Context(), middleware.UserIDFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Joining updated", res)
}

func (h *GuruHandler) RemoveStudent(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	err = h.useCase.RemoveStudent(c.Context(), middleware.UserIDFrom(c), classID, studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Student removed", nil)
}

func (h *GuruHandler) Teachers(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	res, err := h.useCase.Teachers(c.Context(), middleware.UserIDFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) AddTeacher(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	var req dto.AddTeacherRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.AddTeacher(c.Context(), middleware.UserIDFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Teacher added", res)
}

func (h *GuruHandler) CreateClass(c fiber.Ctx) error {
	var req dto.CreateClassRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.CreateClass(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Class created", res)
}

func (h *GuruHandler) Profile(c fiber.Ctx) error {
	res, err := h.useCase.Profile(c.Context(), middleware.UserIDFrom(c))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) ProfileDetail(c fiber.Ctx) error {
	res, err := h.useCase.ProfileDetail(c.Context(), middleware.UserIDFrom(c))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) UpdateProfile(c fiber.Ctx) error {
	var req dto.UpdateGuruProfileRequest

	err := validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateProfile(c.Context(), middleware.UserIDFrom(c), req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Profile updated", res)
}

func optionalUUID(c fiber.Ctx, name string) (*uuid.UUID, error) {
	raw := c.Query(name)
	if raw == "" {
		return nil, nil
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperror.Validation(map[string]string{name: "invalid UUID format"})
	}

	return &parsed, nil
}

func (h *GuruHandler) RemoveTeacher(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	teacherID, err := uuid.Parse(c.Params("teacherId"))
	if err != nil {
		return apperror.Validation(map[string]string{"teacherId": "invalid UUID format"})
	}

	err = h.useCase.RemoveTeacher(c.Context(), middleware.UserIDFrom(c), classID, teacherID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Teacher removed", nil)
}

func (h *GuruHandler) TransferOwnership(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	var req dto.TransferOwnershipRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.TransferOwnership(c.Context(), middleware.UserIDFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Ownership handed over", res)
}

func (h *GuruHandler) MoveRequests(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	res, err := h.useCase.MoveRequests(c.Context(), middleware.UserIDFrom(c), classID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *GuruHandler) AcceptMove(c fiber.Ctx) error {
	classID, requestID, err := moveParams(c)
	if err != nil {
		return err
	}

	err = h.useCase.AcceptMove(c.Context(), middleware.UserIDFrom(c), classID, requestID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Move accepted", nil)
}

func (h *GuruHandler) RejectMove(c fiber.Ctx) error {
	classID, requestID, err := moveParams(c)
	if err != nil {
		return err
	}

	err = h.useCase.RejectMove(c.Context(), middleware.UserIDFrom(c), classID, requestID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Move rejected", nil)
}

func moveParams(c fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	requestID, err := uuid.Parse(c.Params("requestId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, apperror.Validation(map[string]string{"requestId": "invalid UUID format"})
	}

	return classID, requestID, nil
}

func (h *GuruHandler) StudentHistory(c fiber.Ctx) error {
	classID, err := uuid.Parse(c.Params("classId"))
	if err != nil {
		return apperror.Validation(map[string]string{"classId": "invalid UUID format"})
	}

	studentID, err := uuid.Parse(c.Params("studentId"))
	if err != nil {
		return apperror.Validation(map[string]string{"studentId": "invalid UUID format"})
	}

	res, err := h.useCase.StudentHistory(c.Context(), middleware.UserIDFrom(c), classID, studentID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}
