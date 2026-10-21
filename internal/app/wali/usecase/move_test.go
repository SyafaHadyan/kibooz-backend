package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
)

func TestRequestMoveAsksWithTheCodeInUpperCaseAndShowsTheRequest(t *testing.T) {
	repo := parentRepo()
	childID, classID := uuid.New(), uuid.New()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	repo.moveRow = enrollment.ParentRow{
		ID: uuid.New(), StudentID: childID, ClassID: classID, ClassName: "Kelas Melati", Status: constants.EnrollmentPending, RequestedAt: at,
	}

	res, err := newUseCase(repo, &fakeCache{}).RequestMove(context.Background(), uuid.New(), childID, dto.MoveRequestInput{ClassCode: " ab12cd34 "})
	require.NoError(t, err)

	require.Equal(t, "AB12CD34", repo.moveCode)
	require.Equal(t, childID, repo.moveChild)
	require.Equal(t, repo.wali.ID, repo.moveParent)
	require.Equal(t, "pending", res.Status)
	require.Equal(t, "Kelas Melati", res.ClassName)
	require.Equal(t, at.UTC(), res.RequestedAt)
	require.Nil(t, res.DecidedAt)
}

func TestMoveRoutesKeepTheAnswersOfTheRulesAndHideOtherErrors(t *testing.T) {
	ctx := context.Background()

	for _, rule := range []*apperror.Error{
		apperror.ErrStudentNotFound, apperror.ErrClassCodeNotFound, apperror.ErrAlreadyInClass, apperror.ErrMovePending,
		apperror.ErrClassFull, apperror.ErrClassJoiningClosed, apperror.ErrMoveNotFound,
	} {
		repo := parentRepo()
		repo.moveErr = rule

		_, err := newUseCase(repo, &fakeCache{}).RequestMove(ctx, uuid.New(), uuid.New(), dto.MoveRequestInput{ClassCode: "ABCD"})
		requireAppError(t, err, rule)

		err = newUseCase(repo, &fakeCache{}).CancelMove(ctx, uuid.New(), uuid.New(), uuid.New())
		requireAppError(t, err, rule)

		_, err = newUseCase(repo, &fakeCache{}).MoveRequests(ctx, uuid.New(), uuid.New())
		requireAppError(t, err, rule)
	}

	repo := parentRepo()
	repo.moveErr = errors.New("connection reset")

	_, err := newUseCase(repo, &fakeCache{}).RequestMove(ctx, uuid.New(), uuid.New(), dto.MoveRequestInput{ClassCode: "ABCD"})
	requireAppError(t, err, apperror.Internal(repo.moveErr))
}

func TestMoveRoutesNeedAParentProfile(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	useCase := newUseCase(repo, &fakeCache{})

	_, err := useCase.RequestMove(ctx, uuid.New(), uuid.New(), dto.MoveRequestInput{ClassCode: "ABCD"})
	requireAppError(t, err, apperror.ErrProfileNotFound)
	requireAppError(t, useCase.CancelMove(ctx, uuid.New(), uuid.New(), uuid.New()), apperror.ErrProfileNotFound)

	_, err = useCase.MoveRequests(ctx, uuid.New(), uuid.New())
	requireAppError(t, err, apperror.ErrProfileNotFound)
}

func TestMoveRequestsListsWhatWaitsAndWhatWasRejected(t *testing.T) {
	repo := parentRepo()
	decided := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	waiting := enrollment.ParentRow{ID: uuid.New(), ClassName: "Kelas A", Status: constants.EnrollmentPending, RequestedAt: decided.Add(time.Hour)}
	rejected := enrollment.ParentRow{
		ID: uuid.New(), ClassName: "Kelas B", Status: constants.EnrollmentRejected, RequestedAt: decided.Add(-time.Hour), DecidedAt: &decided,
	}
	repo.moveRows = []enrollment.ParentRow{waiting, rejected}

	res, err := newUseCase(repo, &fakeCache{}).MoveRequests(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Len(t, res.Requests, 2)
	require.Nil(t, res.Requests[0].DecidedAt)
	require.Equal(t, "rejected", res.Requests[1].Status)
	require.Equal(t, decided, *res.Requests[1].DecidedAt)

	repo.moveRows = nil

	empty, err := newUseCase(repo, &fakeCache{}).MoveRequests(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, empty.Requests, "an empty list is [] and not null")
	require.Empty(t, empty.Requests)
}

func TestCancelMoveForwardsTheRequestOfTheParent(t *testing.T) {
	repo := parentRepo()
	childID, requestID := uuid.New(), uuid.New()

	require.NoError(t, newUseCase(repo, &fakeCache{}).CancelMove(context.Background(), uuid.New(), childID, requestID))
	require.Equal(t, requestID, repo.moveCancel)
	require.Equal(t, childID, repo.moveChild)
	require.Equal(t, repo.wali.ID, repo.moveParent)
}

func TestMoveRoutesHideTheFailureOfTheParentLookup(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{waliErr: errors.New("connection reset")}
	useCase := newUseCase(repo, &fakeCache{})

	_, err := useCase.RequestMove(ctx, uuid.New(), uuid.New(), dto.MoveRequestInput{ClassCode: "ABCD"})
	requireAppError(t, err, apperror.Internal(repo.waliErr))
	requireAppError(t, useCase.CancelMove(ctx, uuid.New(), uuid.New(), uuid.New()), apperror.Internal(repo.waliErr))

	_, err = useCase.MoveRequests(ctx, uuid.New(), uuid.New())
	requireAppError(t, err, apperror.Internal(repo.waliErr))
}

func TestPreviousClassesShowWhatTheChildHadWhenItLeft(t *testing.T) {
	repo := parentRepo()
	childID, classID := uuid.New(), uuid.New()
	started := time.Date(2026, 7, 13, 0, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	ended := started.AddDate(0, 3, 0)
	repo.previous = []enrollment.PreviousRow{{
		ID: uuid.New(), ClassID: classID, ClassName: "Kelas Melati", GradeLevel: "Class A", SchoolName: "TK Pertiwi", AcademicYear: "2026/2027",
		ActivatedAt: started, EndedAt: ended, FinalPoints: 120, FinalSeason: 45, FinalRank: 3, FinalScans: 17,
	}}

	res, err := newUseCase(repo, &fakeCache{}).PreviousClasses(context.Background(), uuid.New(), childID)
	require.NoError(t, err)

	require.Equal(t, childID, repo.moveChild)
	require.Equal(t, repo.wali.ID, repo.moveParent)
	require.Equal(t, []dto.PreviousClass{{
		ClassID: classID, ClassName: "Kelas Melati", GradeLevel: "Class A", SchoolName: "TK Pertiwi", AcademicYear: "2026/2027",
		StartedAt: started.UTC(), EndedAt: ended.UTC(), TotalPoints: 120, SeasonPoints: 45, ClassRank: 3, TotalScans: 17,
	}}, res.Classes)

	repo.previous = nil

	empty, err := newUseCase(repo, &fakeCache{}).PreviousClasses(context.Background(), uuid.New(), childID)
	require.NoError(t, err)
	require.NotNil(t, empty.Classes, "a child that never moved gets [] and not null")
	require.Empty(t, empty.Classes)
}

func TestPreviousClassesKeepTheAnswersOfTheRulesAndHideOtherErrors(t *testing.T) {
	ctx := context.Background()

	repo := parentRepo()
	repo.moveErr = apperror.ErrStudentNotFound

	_, err := newUseCase(repo, &fakeCache{}).PreviousClasses(ctx, uuid.New(), uuid.New())
	requireAppError(t, err, apperror.ErrStudentNotFound)

	repo.moveErr = errors.New("connection reset")

	_, err = newUseCase(repo, &fakeCache{}).PreviousClasses(ctx, uuid.New(), uuid.New())
	requireAppError(t, err, apperror.Internal(repo.moveErr))

	_, err = newUseCase(&fakeRepo{}, &fakeCache{}).PreviousClasses(ctx, uuid.New(), uuid.New())
	requireAppError(t, err, apperror.ErrProfileNotFound)

	_, err = newUseCase(&fakeRepo{waliErr: errors.New("connection reset")}, &fakeCache{}).PreviousClasses(ctx, uuid.New(), uuid.New())
	requireAppError(t, err, apperror.Internal(errors.New("connection reset")))
}
