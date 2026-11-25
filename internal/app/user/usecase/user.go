// Package usecase holds the profile business rules
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/googleauth"
	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/passwords"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
)

const (
	bcryptMaxBytes = 72

	// freshGoogleWindow is how old the Google token of an action that changes how the account signs in may be
	freshGoogleWindow = 5 * time.Minute

	// a parent can have many trash photos, so the files of a deleted account are removed a few at a time
	discardConcurrency = 8
)

type UserUseCaseItf interface {
	// UploadAvatar stores the image for the caller, or for one of their children when studentID is set
	UploadAvatar(ctx context.Context, userID uuid.UUID, role constants.Role, studentID *uuid.UUID, data []byte) (dto.AvatarResponse, error)
	// DeleteAccount soft deletes the caller after checking their password. A parent's children are deleted with them.
	DeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.DeleteAccountRequest) error
	// ChangePassword swaps the password of the caller after checking the current one and ends every session, so each
	// device has to sign in again with the new password.
	ChangePassword(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.ChangePasswordRequest) error
	// SetPassword gives an account that signs in with Google its first password, after a fresh token of its Google account, and ends
	// every session
	SetPassword(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.SetPasswordRequest) error
	// LinkGoogle adds a Google account to an account that has none, after the password and a fresh token of that Google account
	LinkGoogle(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.LinkGoogleRequest) error
	// UnlinkGoogle removes the Google account of an account that has a password, after the password, and ends every session
	UnlinkGoogle(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.UnlinkGoogleRequest) error
}

type UserUseCase struct {
	repo    repository.UserDBItf
	storage s3.StorageItf
	cache   redis.CacheItf
	// hasher is shared with the other use cases, because its slots are the budget of the whole process
	hasher *passwords.Hasher
	// google checks the tokens of the actions that need a fresh Google sign in, and is nil when Google Sign-In is not set up
	google googleauth.Verifier
	now    func() time.Time
}

// Option changes how the use case is built
type Option func(*UserUseCase)

// WithGoogleVerifier sets what checks the Google tokens of the actions that need one
func WithGoogleVerifier(verifier googleauth.Verifier) Option {
	return func(u *UserUseCase) { u.google = verifier }
}

func NewUserUseCase(
	repo repository.UserDBItf, storage s3.StorageItf, cache redis.CacheItf, hasher *passwords.Hasher, options ...Option,
) UserUseCaseItf {
	u := &UserUseCase{repo: repo, storage: storage, cache: cache, hasher: hasher, now: time.Now}

	for _, option := range options {
		option(u)
	}

	return u
}

func (u *UserUseCase) UploadAvatar(
	ctx context.Context, userID uuid.UUID, role constants.Role, studentID *uuid.UUID, data []byte,
) (dto.AvatarResponse, error) {
	if !u.storage.Enabled() {
		return dto.AvatarResponse{}, apperror.ErrStorageDisabled
	}

	image, err := imageutil.Inspect(data, constants.AvatarMaxBytes)
	if err != nil {
		return dto.AvatarResponse{}, err
	}

	if studentID != nil && role != constants.RoleWali {
		return dto.AvatarResponse{}, apperror.ErrForbidden
	}

	var waliID uuid.UUID

	if studentID != nil {
		found, err := u.repo.FindWaliIDByUserID(ctx, userID)
		if err != nil {
			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		if found == nil {
			return dto.AvatarResponse{}, apperror.ErrProfileNotFound
		}

		waliID = *found
	}

	if studentID == nil {
		user, err := u.repo.FindUserByID(ctx, userID)
		if err != nil {
			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		if user == nil {
			return dto.AvatarResponse{}, apperror.ErrProfileNotFound
		}
	}

	owner := userID
	if studentID != nil {
		owner = *studentID
	}

	key := fmt.Sprintf("%s/%s/%s%s", constants.AvatarDirectory, owner, uuid.NewString(), image.Extension)

	url, err := u.storage.Upload(ctx, key, image.ContentType, image.Data)
	if err != nil {
		return dto.AvatarResponse{}, err
	}

	if studentID == nil {
		previous, found, err := u.repo.UpdateUserAvatar(ctx, userID, url)
		if err != nil {
			if u.avatarCommitted(ctx, key, url, func(ctx context.Context) (string, bool, error) {
				return u.repo.UserAvatar(ctx, userID)
			}) {
				return dto.AvatarResponse{AvatarURL: url}, nil
			}

			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		if !found {
			// the account was deleted after the check above, so the file belongs to nobody
			s3.Discard(ctx, u.storage, key)

			return dto.AvatarResponse{}, apperror.ErrProfileNotFound
		}

		u.discardReplaced(ctx, previous)

		return dto.AvatarResponse{AvatarURL: url}, nil
	}

	classID, previous, err := u.repo.UpdateStudentAvatar(ctx, waliID, *studentID, url)
	if err != nil {
		var committedClass uuid.UUID

		if u.avatarCommitted(ctx, key, url, func(ctx context.Context) (string, bool, error) {
			current, class, found, readErr := u.repo.StudentAvatar(ctx, waliID, *studentID)
			committedClass = class

			return current, found, readErr
		}) {
			// the cache is optional, entries also expire by themselves
			_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+committedClass.String())

			return dto.AvatarResponse{AvatarURL: url}, nil
		}

		return dto.AvatarResponse{}, apperror.Internal(err)
	}

	if classID == nil {
		// the child is not this parent's, so the file belongs to nobody
		s3.Discard(ctx, u.storage, key)

		return dto.AvatarResponse{}, apperror.ErrStudentNotFound
	}

	u.discardReplaced(ctx, previous)

	// the cache is optional, entries also expire by themselves
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	return dto.AvatarResponse{AvatarURL: url}, nil
}

// avatarSettleTimeout bounds the read that tells whether a failed avatar update was committed after all
const avatarSettleTimeout = 3 * time.Second

// avatarCommitted is for an avatar update that returned an error. The error of a transaction can come from its commit, after the database
// already made the change, for example when the connection drops on the way back, and then the address of the new file is live. So the
// address is read again, and the new file is deleted only when the read shows another address. When the read fails too, the file stays,
// because a leaked file costs a few kilobytes and a deleted file under a live address costs a person the picture. It returns true when
// the update did happen. The file that it replaced is not known then and stays in the bucket.
func (u *UserUseCase) avatarCommitted(
	ctx context.Context, key string, url string, read func(ctx context.Context) (string, bool, error),
) bool {
	// the request may be the reason for the error, for example a client that left, and the read still has to run
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), avatarSettleTimeout)
	defer cancel()

	current, found, err := read(ctx)
	if err != nil {
		log.Printf("the avatar update of %s failed and the address could not be read again, the file stays %v", key, err)

		return false
	}

	if found && current == url {
		log.Printf("the avatar update of %s reported an error but was committed, the file it replaced stays", key)

		return true
	}

	s3.Discard(ctx, u.storage, key)

	return false
}

// discardReplaced removes the file an avatar update replaced. It only touches files this storage serves under the
// avatar directory, so an address that was set some other way is never deleted.
func (u *UserUseCase) discardReplaced(ctx context.Context, previous string) {
	if previous == "" {
		return
	}

	key, ok := u.storage.KeyFromURL(previous)
	if !ok || !inDirectory(key, constants.AvatarDirectory) {
		return
	}

	s3.Discard(ctx, u.storage, key)
}

func (u *UserUseCase) DeleteAccount(
	ctx context.Context, userID uuid.UUID, role constants.Role, req dto.DeleteAccountRequest,
) error {
	user, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if user == nil {
		return apperror.ErrProfileNotFound
	}

	if user.Role != role {
		return apperror.ErrForbidden
	}

	err = u.confirmDeletion(ctx, user, req)
	if err != nil {
		return err
	}

	// a refusal such as SUCCESSOR_REQUIRED is an answer for the caller, anything else is a failure
	deleted, err := u.repo.SoftDeleteAccount(ctx, userID, role, user.PasswordHash, strings.ToLower(strings.TrimSpace(req.SuccessorEmail)), u.fileKey)
	if err != nil {
		var refusal *apperror.Error
		if errors.As(err, &refusal) {
			return refusal
		}

		return apperror.Internal(err)
	}

	// the cache is optional, entries also expire by themselves
	for _, classID := range deleted.ClassIDs {
		_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())
	}

	u.discardFiles(ctx, deleted.FileKeys)

	return nil
}

func (u *UserUseCase) ChangePassword(
	ctx context.Context, userID uuid.UUID, role constants.Role, req dto.ChangePasswordRequest,
) error {
	user, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if user == nil {
		return apperror.ErrProfileNotFound
	}

	if user.Role != role {
		return apperror.ErrForbidden
	}

	if !strings.HasPrefix(user.PasswordHash, "$2") {
		return apperror.ErrPasswordNotSet
	}

	// bcrypt only reads the first 72 bytes, so a longer password could match a shorter one that shares them
	if len([]byte(req.CurrentPassword)) > bcryptMaxBytes {
		return apperror.ErrPasswordIncorrect
	}

	matches, err := u.hasher.Matches(ctx, user.PasswordHash, req.CurrentPassword)
	if err != nil {
		return apperror.As(err)
	}

	if !matches {
		return apperror.ErrPasswordIncorrect
	}

	if len([]byte(req.NewPassword)) > bcryptMaxBytes {
		//nolint:gosec // this is a validation message and not a credential
		return apperror.Validation(map[string]string{"newPassword": "too long, maximum 72 bytes"})
	}

	if req.NewPassword == req.CurrentPassword {
		return apperror.Validation(map[string]string{"newPassword": "must differ from the current password"})
	}

	identities := []string{user.Email, user.FullName}
	if user.PhoneNumber != nil {
		identities = append(identities, *user.PhoneNumber)
	}

	if validation.ContainsIdentity(req.NewPassword, identities...) {
		return apperror.Validation(map[string]string{"newPassword": validation.OwnDetailsMessage})
	}

	hash, err := u.hasher.Hash(ctx, req.NewPassword)
	if err != nil {
		return apperror.As(err)
	}

	changed, err := u.repo.UpdatePassword(ctx, userID, user.PasswordHash, hash)
	if err != nil {
		return apperror.Internal(err)
	}

	// the password was changed from another device while this request hashed, so the one that was sent is out of date
	if !changed {
		return apperror.ErrPasswordIncorrect
	}

	return nil
}

// fileKey picks the avatars and the trash photos of this storage out of the addresses a deleted account held
func (u *UserUseCase) fileKey(url string) (string, bool) {
	key, ok := u.storage.KeyFromURL(url)

	return key, ok && inDirectory(key, constants.AvatarDirectory, constants.TrashDirectory)
}

// cleanupBudget is the longest the response of a deletion waits for the bucket. The files it did not reach stay on the
// queue for the sweep.
const cleanupBudget = 10 * time.Second

// queueTimeout bounds the write that takes deleted files off the queue
const queueTimeout = 5 * time.Second

// discardFiles removes the avatars and the trash photos of a deleted account. Their keys are already on the cleanup
// queue, so a file that cannot be deleted now, or that the time budget did not leave room for, stays there and a later
// sweep removes it. The ones that are gone leave the queue.
func (u *UserUseCase) discardFiles(ctx context.Context, keys []string) {
	var (
		group sync.WaitGroup
		mu    sync.Mutex
		gone  []string
	)

	// the client may give up, the cleanup still has to run, but not for longer than the budget
	budget, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupBudget)
	defer cancel()

	limit := make(chan struct{}, discardConcurrency)

	for _, key := range keys {
		if budget.Err() != nil {
			break
		}

		group.Add(1)

		limit <- struct{}{}

		go func() {
			defer group.Done()
			defer func() { <-limit }()

			err := u.storage.Delete(budget, key)
			if err != nil {
				log.Printf("storage cleanup of %s failed, it will be tried again %v", key, err)

				return
			}

			mu.Lock()
			gone = append(gone, key)
			mu.Unlock()
		}()
	}

	group.Wait()

	// the account is already deleted, so a queue that cannot be updated only means a later sweep deletes the file again
	queueCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), queueTimeout)
	defer stop()

	err := u.repo.UnqueueFileCleanups(queueCtx, gone)
	if err != nil {
		log.Printf("take the deleted storage files off the cleanup queue %v", err)
	}
}

// inDirectory tells whether key lies under one of the directories this service writes to
func inDirectory(key string, directories ...constants.StorageDirectory) bool {
	for _, directory := range directories {
		if strings.HasPrefix(key, string(directory)+"/") {
			return true
		}
	}

	return false
}

// hasPassword tells whether the account has a password, an account that signs in with Google only has a placeholder that is no hash
func hasPassword(user *entity.User) bool {
	return strings.HasPrefix(user.PasswordHash, "$2")
}

// confirmDeletion asks an account that has a password for it, and an account that signs in with Google only for a fresh token of
// its Google account, because there is no password to ask for
func (u *UserUseCase) confirmDeletion(ctx context.Context, user *entity.User, req dto.DeleteAccountRequest) error {
	if !hasPassword(user) {
		if strings.TrimSpace(req.IDToken) == "" {
			return apperror.Validation(map[string]string{"idToken": "is required"})
		}

		return u.confirmGoogle(ctx, user, req.IDToken)
	}

	if req.Password == "" {
		return apperror.Validation(map[string]string{"password": "is required"})
	}

	return u.confirmPassword(ctx, user, req.Password)
}

// confirmPassword checks the password of the account
func (u *UserUseCase) confirmPassword(ctx context.Context, user *entity.User, password string) error {
	// bcrypt only reads the first 72 bytes, so a longer password could match a shorter one that shares them
	if len([]byte(password)) > bcryptMaxBytes {
		return apperror.ErrPasswordIncorrect
	}

	matches, err := u.hasher.Matches(ctx, user.PasswordHash, password)
	if err != nil {
		// the server is busy hashing, or the request ended
		return apperror.As(err)
	}

	if !matches {
		return apperror.ErrPasswordIncorrect
	}

	return nil
}

// confirmGoogle checks that the token is a fresh one of the Google account that is linked to the account
func (u *UserUseCase) confirmGoogle(ctx context.Context, user *entity.User, idToken string) error {
	if user.GoogleSub == nil {
		return apperror.ErrGoogleNotLinked
	}

	claims, err := u.freshGoogleClaims(ctx, idToken)
	if err != nil {
		return err
	}

	if claims.Subject != *user.GoogleSub {
		return apperror.ErrGoogleInvalid
	}

	return nil
}

// freshGoogleClaims verifies the token and refuses one that Google issued more than five minutes ago, so a token that was seen once
// cannot be used later for an action that changes how the account signs in
func (u *UserUseCase) freshGoogleClaims(ctx context.Context, idToken string) (googleauth.Claims, error) {
	if u.google == nil {
		return googleauth.Claims{}, apperror.ErrGoogleUnavailable
	}

	claims, err := u.google.Verify(ctx, strings.TrimSpace(idToken))
	if err != nil {
		switch {
		case errors.Is(err, googleauth.ErrNotConfigured):
			return googleauth.Claims{}, apperror.ErrGoogleUnavailable
		case errors.Is(err, googleauth.ErrInvalidToken):
			return googleauth.Claims{}, apperror.ErrGoogleInvalid
		default:
			return googleauth.Claims{}, apperror.ErrGoogleUnavailable.WithErr(err)
		}
	}

	if !claims.FreshWithin(u.now(), freshGoogleWindow) {
		return googleauth.Claims{}, apperror.ErrGoogleStale
	}

	return claims, nil
}

func (u *UserUseCase) SetPassword(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.SetPasswordRequest) error {
	user, err := u.findOwn(ctx, userID, role)
	if err != nil {
		return err
	}

	if hasPassword(user) {
		return apperror.ErrPasswordAlreadySet
	}

	err = u.confirmGoogle(ctx, user, req.IDToken)
	if err != nil {
		return err
	}

	if len([]byte(req.NewPassword)) > bcryptMaxBytes {
		//nolint:gosec // this is a validation message and not a credential
		return apperror.Validation(map[string]string{"newPassword": "too long, maximum 72 bytes"})
	}

	identities := []string{user.Email, user.FullName}
	if user.PhoneNumber != nil {
		identities = append(identities, *user.PhoneNumber)
	}

	if validation.ContainsIdentity(req.NewPassword, identities...) {
		return apperror.Validation(map[string]string{"newPassword": validation.OwnDetailsMessage})
	}

	hash, err := u.hasher.Hash(ctx, req.NewPassword)
	if err != nil {
		return apperror.As(err)
	}

	// the stored value is the placeholder that was read, so a password that was set in between is not overwritten
	changed, err := u.repo.UpdatePassword(ctx, userID, user.PasswordHash, hash)
	if err != nil {
		return apperror.Internal(err)
	}

	if !changed {
		return apperror.ErrPasswordAlreadySet
	}

	return nil
}

func (u *UserUseCase) LinkGoogle(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.LinkGoogleRequest) error {
	user, err := u.findOwn(ctx, userID, role)
	if err != nil {
		return err
	}

	if user.GoogleSub != nil {
		return apperror.ErrGoogleAlreadyLinked
	}

	// an account without a password and without a link cannot exist, but a refusal is better than a link that nobody confirmed
	if !hasPassword(user) {
		return apperror.ErrPasswordNotSet
	}

	err = u.confirmPassword(ctx, user, req.Password)
	if err != nil {
		return err
	}

	claims, err := u.freshGoogleClaims(ctx, req.IDToken)
	if err != nil {
		return err
	}

	linked, err := u.repo.LinkGoogleSub(ctx, userID, claims.Subject, user.PasswordHash)
	if err != nil {
		var refusal *apperror.Error
		if errors.As(err, &refusal) {
			return refusal
		}

		return apperror.Internal(err)
	}

	if !linked {
		return apperror.ErrGoogleAlreadyLinked
	}

	return nil
}

func (u *UserUseCase) UnlinkGoogle(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.UnlinkGoogleRequest) error {
	user, err := u.findOwn(ctx, userID, role)
	if err != nil {
		return err
	}

	if user.GoogleSub == nil {
		return apperror.ErrGoogleNotLinked
	}

	// without a password the account would have no way in once the link is gone
	if !hasPassword(user) {
		return apperror.ErrPasswordNotSet
	}

	err = u.confirmPassword(ctx, user, req.Password)
	if err != nil {
		return err
	}

	unlinked, err := u.repo.UnlinkGoogleSub(ctx, userID, *user.GoogleSub, user.PasswordHash)
	if err != nil {
		return apperror.Internal(err)
	}

	if !unlinked {
		return apperror.ErrGoogleNotLinked
	}

	return nil
}

// findOwn returns the account of the caller, or the error that the routes answer
func (u *UserUseCase) findOwn(ctx context.Context, userID uuid.UUID, role constants.Role) (*entity.User, error) {
	user, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if user == nil {
		return nil, apperror.ErrProfileNotFound
	}

	if user.Role != role {
		return nil, apperror.ErrForbidden
	}

	return user, nil
}
