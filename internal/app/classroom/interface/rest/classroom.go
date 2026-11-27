// Package rest exposes the learning videos and the class forum over HTTP
package rest

import (
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/kiboozapp/kibooz-backend/internal/app/classroom/usecase"
	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/infra/validation"
	"github.com/kiboozapp/kibooz-backend/internal/middleware"
	"github.com/kiboozapp/kibooz-backend/internal/pagination"
	"github.com/kiboozapp/kibooz-backend/internal/response"
)

type ClassroomHandler struct {
	useCase usecase.ClassroomUseCaseItf
}

func NewClassroomHandler(router fiber.Router, mw middleware.MiddlewareItf, useCase usecase.ClassroomUseCaseItf) {
	handler := ClassroomHandler{useCase: useCase}

	group := router.Group("/classes/:classId", mw.Authentication)

	members := mw.RequireRole(constants.RoleGuru, constants.RoleWali)
	// a repeat of a request that already created something gets the first response again
	idempotent := mw.Idempotent()

	group.Get("/videos", members, handler.ListVideos)
	group.Post("/videos", mw.RequireRole(constants.RoleGuru), idempotent, handler.AddVideo)
	group.Post("/videos/upload-url", mw.RequireRole(constants.RoleGuru), handler.CreateVideoUpload)
	// before /videos/:videoId, which would take upload-url for a video id
	group.Delete("/videos/upload-url", mw.RequireRole(constants.RoleGuru), handler.DiscardVideoUpload)
	group.Patch("/videos/:videoId", mw.RequireRole(constants.RoleGuru), handler.UpdateVideo)
	group.Delete("/videos/:videoId", mw.RequireRole(constants.RoleGuru), handler.DeleteVideo)
	group.Get("/modules", members, handler.ListModules)
	group.Post("/modules", mw.RequireRole(constants.RoleGuru), idempotent, handler.CreateModule)
	group.Patch("/modules/:moduleId", mw.RequireRole(constants.RoleGuru), handler.UpdateModule)
	group.Delete("/modules/:moduleId", mw.RequireRole(constants.RoleGuru), handler.DeleteModule)
	group.Get("/forum", members, handler.ListThreads)
	group.Post("/forum", members, idempotent, handler.CreateThread)
	group.Patch("/forum/:postId", members, handler.UpdateThread)
	group.Delete("/forum/:postId", members, handler.DeleteThread)
	group.Get("/forum/:postId/replies", members, handler.ListReplies)
	group.Post("/forum/:postId/replies", members, idempotent, handler.CreateReply)
	group.Patch("/forum/:postId/replies/:replyId", members, handler.UpdateReply)
	group.Delete("/forum/:postId/replies/:replyId", members, handler.DeleteReply)
	// the register holds the NISN of every child, so it is for the teachers only
	group.Get("/students", mw.RequireRole(constants.RoleGuru), handler.ListStudents)
	group.Get("/guidance-reports", mw.RequireRole(constants.RoleGuru), handler.ListGuidanceReports)
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

func (h *ClassroomHandler) UpdateVideo(c fiber.Ctx) error {
	classID, videoID, err := classAndVideo(c)
	if err != nil {
		return err
	}

	var req dto.UpdateVideoRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateVideo(c.Context(), middleware.UserIDFrom(c), classID, videoID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Learning video updated", res)
}

func (h *ClassroomHandler) DeleteVideo(c fiber.Ctx) error {
	classID, videoID, err := classAndVideo(c)
	if err != nil {
		return err
	}

	err = h.useCase.DeleteVideo(c.Context(), middleware.UserIDFrom(c), classID, videoID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Learning video deleted", nil)
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

func (h *ClassroomHandler) DiscardVideoUpload(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	err = h.useCase.DiscardVideoUpload(c.Context(), middleware.UserIDFrom(c), classID, c.Query("videoUrl"))
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Upload deleted", nil)
}

func (h *ClassroomHandler) ListModules(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListModules(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func (h *ClassroomHandler) CreateModule(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	var req dto.CreateModuleRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.CreateModule(c.Context(), middleware.UserIDFrom(c), classID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusCreated, "Learning module added", res)
}

func (h *ClassroomHandler) UpdateModule(c fiber.Ctx) error {
	classID, moduleID, err := classAndItem(c, "moduleId")
	if err != nil {
		return err
	}

	var req dto.UpdateModuleRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateModule(c.Context(), middleware.UserIDFrom(c), classID, moduleID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Learning module updated", res)
}

func (h *ClassroomHandler) DeleteModule(c fiber.Ctx) error {
	classID, moduleID, err := classAndItem(c, "moduleId")
	if err != nil {
		return err
	}

	err = h.useCase.DeleteModule(c.Context(), middleware.UserIDFrom(c), classID, moduleID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Learning module deleted", nil)
}

func (h *ClassroomHandler) ListThreads(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	subjectCode, generalOnly, err := parseThreadFilter(c)
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListThreads(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, subjectCode, generalOnly, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
}

func parseThreadFilter(c fiber.Ctx) (*constants.SubjectCode, bool, error) {
	subjectCode, ok := constants.ParseSubjectCode(c.Query("subjectCode"))
	if !ok {
		return nil, false, apperror.Validation(map[string]string{
			"subjectCode": "must be one of SENI-A1, BHS-A1, LOG-A1, MOT-A1, AGM-A1, SOS-A1",
		})
	}

	generalOnly := false
	if raw := c.Query("generalOnly"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, false, apperror.Validation(map[string]string{"generalOnly": "must be true or false"})
		}

		generalOnly = parsed
	}

	if generalOnly && subjectCode != nil {
		return nil, false, apperror.Validation(map[string]string{
			"generalOnly": "cannot be combined with subjectCode",
		})
	}

	return subjectCode, generalOnly, nil
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

func (h *ClassroomHandler) UpdateThread(c fiber.Ctx) error {
	classID, threadID, err := classAndPost(c)
	if err != nil {
		return err
	}

	var req dto.UpdateThreadRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateThread(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, threadID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Forum thread updated", res)
}

func (h *ClassroomHandler) DeleteThread(c fiber.Ctx) error {
	classID, threadID, err := classAndPost(c)
	if err != nil {
		return err
	}

	err = h.useCase.DeleteThread(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, threadID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Forum thread deleted", nil)
}

func (h *ClassroomHandler) UpdateReply(c fiber.Ctx) error {
	classID, threadID, replyID, err := classPostAndReply(c)
	if err != nil {
		return err
	}

	var req dto.UpdateReplyRequest

	err = validation.BindBody(c, &req)
	if err != nil {
		return err
	}

	res, err := h.useCase.UpdateReply(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, threadID, replyID, req)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Forum reply updated", res)
}

func (h *ClassroomHandler) DeleteReply(c fiber.Ctx) error {
	classID, threadID, replyID, err := classPostAndReply(c)
	if err != nil {
		return err
	}

	err = h.useCase.DeleteReply(c.Context(), middleware.UserIDFrom(c), middleware.RoleFrom(c), classID, threadID, replyID)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "Forum reply deleted", nil)
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

func (h *ClassroomHandler) ListGuidanceReports(c fiber.Ctx) error {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return err
	}

	page, err := pagination.Parse(c)
	if err != nil {
		return err
	}

	res, err := h.useCase.ListGuidanceReports(c.Context(), middleware.UserIDFrom(c), classID, page)
	if err != nil {
		return err
	}

	return response.JSON(c, http.StatusOK, "", res)
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

func classPostAndReply(c fiber.Ctx) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	classID, threadID, err := classAndPost(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}

	replyID, err := pathUUID(c, "replyId")
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}

	return classID, threadID, replyID, nil
}

func classAndVideo(c fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	return classAndItem(c, "videoId")
}

func classAndPost(c fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	return classAndItem(c, "postId")
}

func classAndItem(c fiber.Ctx, itemParam string) (uuid.UUID, uuid.UUID, error) {
	classID, err := pathUUID(c, "classId")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	itemID, err := pathUUID(c, itemParam)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return classID, itemID, nil
}

func pathUUID(c fiber.Ctx, name string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, apperror.Validation(map[string]string{name: "invalid UUID format"})
	}

	return parsed, nil
}
