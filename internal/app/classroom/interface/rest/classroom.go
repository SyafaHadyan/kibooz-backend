// Package rest exposes the learning videos and the class forum over HTTP
package rest

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type ClassroomHandler struct {
	useCase usecase.ClassroomUseCaseItf
}

func NewClassroomHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.ClassroomUseCaseItf) {
	handler := ClassroomHandler{useCase: useCase}

	group := router.Group("/classes/:classId", mw.Authentication)

	members := mw.RequireRole(constants.RoleGuru, constants.RoleWali)

	group.Get("/videos", members, handler.ListVideos)
	group.Post("/videos", mw.RequireRole(constants.RoleGuru), handler.AddVideo)
	group.Post("/videos/upload-url", mw.RequireRole(constants.RoleGuru), handler.CreateVideoUpload)
	group.Get("/forum", members, handler.ListThreads)
	group.Post("/forum", members, handler.CreateThread)
	group.Get("/forum/:postId/replies", members, handler.ListReplies)
	group.Post("/forum/:postId/replies", members, handler.CreateReply)
	// the register holds the NISN of every child, so it is for the teachers only
	group.Get("/students", mw.RequireRole(constants.RoleGuru), handler.ListStudents)
}

func (h *ClassroomHandler) ListVideos(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListVideos(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ClassroomHandler) AddVideo(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	var req dto.AddVideoRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.AddVideo(c.Context(), middleware.UserIDFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Learning video added", res)
}

func (h *ClassroomHandler) CreateVideoUpload(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	var req dto.VideoUploadRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.CreateVideoUpload(c.Context(), middleware.UserIDFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Upload URL created", res)
}

func (h *ClassroomHandler) ListThreads(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListThreads(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ClassroomHandler) CreateThread(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	var req dto.CreateThreadRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.CreateThread(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Forum thread created", res)
}

func (h *ClassroomHandler) ListReplies(c fiber.Ctx) error {
	classID, threadID, err := classAndPost(c)
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListReplies(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, threadID, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ClassroomHandler) CreateReply(c fiber.Ctx) error {
	classID, threadID, err := classAndPost(c)
	if err != nil {
		return err
	}

	var req dto.CreateReplyRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.CreateReply(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, threadID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Forum reply added", res)
}

func (h *ClassroomHandler) ListStudents(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListStudents(c.Context(), middleware.UserIDFrom(c), classID, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func classAndPost(c fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	postID, err := pathUUID(c, "postId")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return classID, postID, nil
}

func pathUUID(c fiber.Ctx, name string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, apperror.Validation(map[string]string{name: "invalid UUID format"})
	}

	return parsed, nil
}
