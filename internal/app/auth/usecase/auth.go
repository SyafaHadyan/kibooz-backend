// Package usecase holds the authentication business rules
package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kiboozapp/kibooz-backend/internal/app/auth/repository"
	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/errorreport"
	"github.com/kiboozapp/kibooz-backend/internal/googleauth"
	"github.com/kiboozapp/kibooz-backend/internal/infra/devicetoken"
	"github.com/kiboozapp/kibooz-backend/internal/infra/env"
	"github.com/kiboozapp/kibooz-backend/internal/infra/jwt"
	"github.com/kiboozapp/kibooz-backend/internal/infra/passwords"
	"github.com/kiboozapp/kibooz-backend/internal/infra/redis"
	"github.com/kiboozapp/kibooz-backend/internal/infra/validation"
	"github.com/kiboozapp/kibooz-backend/internal/mailer"
)

const (
	bcryptMaxBytes = 72

	// releaseTimeout is how long giving back the send of a mail that failed may take
	releaseTimeout = 3 * time.Second

	// minFullNameRunes is the shortest name of a person, the same minimum the request validator uses
	minFullNameRunes = 2
)

type AuthUseCaseItf interface {
	Register(ctx context.Context, req dto.RegisterRequest) (dto.AuthResponse, error)
	Login(ctx context.Context, req dto.LoginRequest) (dto.AuthResponse, error)
	GoogleLogin(ctx context.Context, req dto.GoogleLoginRequest) (dto.GoogleAuthResponse, error)
	GoogleRegister(ctx context.Context, req dto.GoogleRegisterRequest) (dto.AuthResponse, error)
	SetGoogleVerifier(verifier googleauth.Verifier)
	// GoogleVerifier is the verifier that the use case checks tokens with, so the use cases of the profile check them the same way
	GoogleVerifier() googleauth.Verifier
	Refresh(ctx context.Context, refreshToken string) (dto.AuthResponse, error)
	Logout(ctx context.Context, refreshToken string) error
	// VerifyEmail checks the code of a new account and opens its first session
	VerifyEmail(ctx context.Context, req dto.VerifyEmailRequest) (dto.AuthResponse, error)
	// ResendVerification sends a new code to an account that still waits for its email
	ResendVerification(ctx context.Context, req dto.ResendVerificationRequest) (dto.VerificationResponse, error)
	// RetryVerification mails a new code to an account whose last mail failed. It returns mailer.ErrQuota when the mail service
	// refused for its quota, and nil when the retry is over or not needed, so the job that calls it stops only for a quota.
	RetryVerification(ctx context.Context, userID uuid.UUID) error
	// Collectors are the metrics of the mail that the use case sends
	Collectors() []prometheus.Collector
}

type AuthUseCase struct {
	repo  repository.AuthDBItf
	jwt   jwt.JWTItf
	cache redis.CacheItf
	cfg   *env.Env
	now   func() time.Time

	// hasher is shared with the other use cases, because its slots are the budget of the whole process
	hasher *passwords.Hasher

	devices *devicetoken.Tokens

	google *googleauth.SwitchableVerifier

	mail mailer.Sender
	// removeAccount removes an account that waited too long for its code, nil when none is ever replaced
	removeAccount AccountRemover
	// mails counts the codes that were handed to the mail service, by result
	mails *prometheus.CounterVec
	// report sends a failure to the error reports, a field for the tests
	report func(error)
}

// AccountRemover removes an account that waits for the code of its email, with everything that hangs on it
type AccountRemover func(ctx context.Context, account repository.PendingAccount, before time.Time) error

// Option changes how the use case is built, and the tests use it to put a fake in place of a part
type Option func(*AuthUseCase)

// WithAccountRemover sets what removes an account that waited too long for its code. Without it no account is ever replaced.
func WithAccountRemover(remove AccountRemover) Option {
	return func(u *AuthUseCase) { u.removeAccount = remove }
}

// WithMailer sets what sends the mail. Without it nothing can be sent and a new account has to ask for its code again.
func WithMailer(sender mailer.Sender) Option {
	return func(u *AuthUseCase) { u.mail = sender }
}

func NewAuthUseCase(
	repo repository.AuthDBItf, jwt jwt.JWTItf, cache redis.CacheItf, cfg *env.Env, hasher *passwords.Hasher, options ...Option,
) AuthUseCaseItf {
	var clientIDs []string
	if cfg != nil {
		clientIDs = cfg.GoogleClientIDs
	}

	u := &AuthUseCase{
		repo: repo, jwt: jwt, cache: cache, cfg: cfg, now: time.Now, devices: devicetoken.New(cfg), hasher: hasher,
		google: googleauth.NewSwitchableVerifier(googleauth.NewVerifier(clientIDs)),
		mail:   noMail{},
		mails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "verification_emails_total", Help: "Verification codes handed to the mail service, by result.",
		}, []string{"result"}),
		report: func(err error) { errorreport.Report(err, errorreport.Info{}) },
	}

	for _, option := range options {
		option(u)
	}

	return u
}

// noMail is the sender of a use case that was built without one
type noMail struct{}

func (noMail) SendVerificationCode(context.Context, string, string, int) error {
	return mailer.ErrDisabled
}

// Collectors are the metrics of the mail that the use case sends
func (u *AuthUseCase) Collectors() []prometheus.Collector {
	// the series exist from the start, so a result that never happened still shows
	for _, result := range []string{"sent", "failed"} {
		u.mails.WithLabelValues(result)
	}

	return []prometheus.Collector{u.mails}
}

// checkNames refuses a name that is only spaces or that is too short once the spaces are gone. The validator counts the text as it was
// sent, and the names are stored trimmed, so "  " would pass it and be stored as an empty name.
func checkNames(req dto.RegisterRequest) map[string]string {
	details := map[string]string{}

	check := func(field string, value string, minRunes int) {
		trimmed := strings.TrimSpace(value)

		switch {
		case trimmed == "":
			details[field] = "is required"
		case utf8.RuneCountInString(trimmed) < minRunes:
			details[field] = "too short or too small, minimum " + strconv.Itoa(minRunes)
		}
	}

	check("fullName", req.FullName, minFullNameRunes)

	if req.Role == constants.RoleGuru && req.Class != nil {
		check("class.name", req.Class.Name, 1)
	}

	if req.Role == constants.RoleWali && req.Student != nil {
		check("student.fullName", req.Student.FullName, minFullNameRunes)
	}

	return details
}

func (u *AuthUseCase) Register(ctx context.Context, req dto.RegisterRequest) (dto.AuthResponse, error) {
	details := checkNames(req)
	if len(details) > 0 {
		return dto.AuthResponse{}, apperror.Validation(details)
	}

	if len([]byte(req.Password)) > bcryptMaxBytes {
		//nolint:gosec // this is a validation message and not a credential
		return dto.AuthResponse{}, apperror.Validation(map[string]string{"password": "too long, maximum 72 bytes"})
	}

	// a password that holds the person's own details is refused before the hash too, like the names above
	identities := []string{req.Email, req.PhoneNumber, req.WhatsappNumber, req.NIP, req.FullName}
	if req.Student != nil {
		identities = append(identities, req.Student.NISN, req.Student.FullName)
	}

	if validation.ContainsIdentity(req.Password, identities...) {
		return dto.AuthResponse{}, apperror.Validation(map[string]string{"password": validation.OwnDetailsMessage})
	}

	// a code that no class uses is refused before the password hash, which costs far more than the lookup
	// and would otherwise be spent on every wrong guess. The transaction of CreateWali looks the class up again.
	if req.Role == constants.RoleWali {
		found, err := u.repo.ClassCodeExists(ctx, normalizeClassCode(req.ClassCode))
		if err != nil {
			return dto.AuthResponse{}, apperror.Internal(err)
		}

		if !found {
			return dto.AuthResponse{}, apperror.ErrClassCodeNotFound
		}
	}

	// an email, a NIP or a NISN that an account already uses is refused before the password hash too. The unique indexes still
	// decide when two registrations race, so this only saves the hash on the common repeat.
	nip, nisn := "", ""
	if req.Role == constants.RoleGuru {
		nip = strings.TrimSpace(req.NIP)
	}

	if req.Role == constants.RoleWali && req.Student != nil {
		nisn = strings.TrimSpace(req.Student.NISN)
	}

	stale, err := u.replaceable(ctx, normalizeEmail(req.Email), nip, nisn)
	if err != nil {
		return dto.AuthResponse{}, apperror.As(err)
	}

	hash, err := u.hasher.Hash(ctx, req.Password)
	if err != nil {
		// the server is busy hashing, or the request ended
		return dto.AuthResponse{}, apperror.As(err)
	}

	// an old account is only removed once nothing else stands in the way of the new one
	classCode := ""
	if req.Role == constants.RoleWali {
		classCode = normalizeClassCode(req.ClassCode)
	}

	err = u.replace(ctx, stale, normalizeEmail(req.Email), nip, nisn, classCode)
	if err != nil {
		return dto.AuthResponse{}, apperror.As(err)
	}

	user := &entity.User{
		ID:           uuid.New(),
		Email:        normalizeEmail(req.Email),
		PasswordHash: hash,
		Role:         req.Role,
		FullName:     strings.TrimSpace(req.FullName),
		PhoneNumber:  optional(req.PhoneNumber),

		// the account waits for the code of its email and gets no session until the code is right
		EmailVerificationPending: u.cfg.EmailVerificationRequired,
	}

	// the first refresh token is stored with the account, so a failure after the account exists cannot leave it without a
	// session, which would send the retry of a registration into a conflict on an email the person never got a session for.
	// An account that waits for its code gets none, and asks for a new code with its password if the mail did not arrive.
	var (
		raw     string
		session *entity.RefreshToken
	)

	if !user.EmailVerificationPending {
		raw, session = u.newRefreshToken()
		session.UserID = user.ID
	}

	switch req.Role {
	case constants.RoleGuru:
		err = u.registerGuru(ctx, user, req, session)
	case constants.RoleWali:
		err = u.registerWali(ctx, user, req, session)
	default:
		return dto.AuthResponse{}, apperror.Validation(map[string]string{"role": "must be one of GURU WALI"})
	}

	if err != nil {
		return dto.AuthResponse{}, err
	}

	if user.EmailVerificationPending {
		return u.pendingRegistration(ctx, user)
	}

	res, err := u.buildResponse(user, raw)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	return u.withDeviceToken(res, user, "")
}

// replaceable is RegistrationTaken, except that an email or a NIP does not count when only accounts hold it that have waited for
// their code for more than a day and never signed in. It returns those accounts, which replace removes. When anything else is taken
// too, it returns the conflict and nothing is to be removed. It only works while the verification of the email is required,
// because an account that waits for a code can sign in when it is not.
func (u *AuthUseCase) replaceable(ctx context.Context, email string, nip string, nisn string) ([]repository.PendingAccount, error) {
	err := u.repo.RegistrationTaken(ctx, email, nip, nisn)
	if err == nil || !u.cfg.EmailVerificationRequired || u.removeAccount == nil {
		return nil, err
	}

	if code := apperror.As(err).Code; code != apperror.ErrEmailTaken.Code && code != apperror.ErrNIPTaken.Code {
		return nil, err
	}

	stale, lookupErr := u.repo.StalePendingAccounts(ctx, email, nip, u.pendingReplaceBefore())
	if lookupErr != nil || len(stale) == 0 {
		return nil, err
	}

	ids := make([]uuid.UUID, len(stale))
	for i, account := range stale {
		ids[i] = account.ID
	}

	if rest := u.repo.RegistrationTaken(ctx, email, nip, nisn, ids...); rest != nil {
		return nil, rest
	}

	return stale, nil
}

// replace removes the accounts that replaceable returned. It first asks again for everything that could refuse the new account,
// so that a registration that is bound to fail removes nothing. A failure after that, such as a second account that cannot be
// removed, leaves the first one removed, which the sweep would have removed anyway. A classCode is empty for a teacher. When an
// account cannot be removed, or something is taken after the removal, it returns the conflict.
func (u *AuthUseCase) replace(
	ctx context.Context, stale []repository.PendingAccount, email string, nip string, nisn string, classCode string,
) error {
	if len(stale) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, len(stale))
	for i, account := range stale {
		ids[i] = account.ID
	}

	// time passed since the first look, such as the hash of the password
	err := u.repo.RegistrationTaken(ctx, email, nip, nisn, ids...)
	if err == nil && classCode != "" {
		err = u.repo.ClassTakesChildren(ctx, classCode)
	}

	if err != nil {
		return err
	}

	for _, account := range stale {
		err = u.removeAccount(ctx, account, u.pendingReplaceBefore())
		if err != nil {
			log.Printf("an account that waited for its code was not replaced %v", err)

			break
		}
	}

	return u.repo.RegistrationTaken(ctx, email, nip, nisn)
}

func (u *AuthUseCase) pendingReplaceBefore() time.Time {
	return u.now().Add(-constants.PendingReplaceAfterHours * time.Hour)
}

// pendingRegistration answers the registration of an account that waits for its code, after it sent the first one. The account
// exists whatever happens to the mail, so a failure here is told to the client in the answer and is not a failed registration.
func (u *AuthUseCase) pendingRegistration(ctx context.Context, user *entity.User) (dto.AuthResponse, error) {
	sent, err := u.issueVerification(ctx, user)
	if err != nil {
		log.Printf("the first verification code of a new account was not issued %v", err)
	}

	res := dto.AuthResponse{User: userResponse(user), VerificationRequired: true, Verification: verificationInfo(sent && err == nil)}

	return u.withDeviceToken(res, user, "")
}

func (u *AuthUseCase) registerGuru(ctx context.Context, user *entity.User, req dto.RegisterRequest, session *entity.RefreshToken) error {
	school := firstNonEmpty(req.Class.SchoolName, req.SchoolName, constants.DefaultSchoolName)

	guru := &entity.Guru{
		ID:         uuid.New(),
		UserID:     user.ID,
		NIP:        optional(req.NIP),
		SchoolName: school,
	}

	class := &entity.Class{
		ID:           uuid.New(),
		SchoolName:   school,
		Name:         strings.TrimSpace(req.Class.Name),
		GradeLevel:   firstNonEmpty(req.Class.GradeLevel, constants.DefaultGradeLevel),
		AcademicYear: firstNonEmpty(req.Class.AcademicYear, constants.DefaultAcademicYr),
		JoinOpen:     true,
	}

	return wrap(u.repo.CreateGuru(ctx, user, guru, class, session))
}

func (u *AuthUseCase) registerWali(ctx context.Context, user *entity.User, req dto.RegisterRequest, session *entity.RefreshToken) error {
	wali := &entity.Wali{
		ID:             uuid.New(),
		UserID:         user.ID,
		Address:        optional(req.Address),
		WhatsappNumber: optional(req.WhatsappNumber),
	}

	student := &entity.Student{
		ID:       uuid.New(),
		NISN:     strings.TrimSpace(req.Student.NISN),
		FullName: strings.TrimSpace(req.Student.FullName),
	}

	classID, err := u.repo.CreateWali(ctx, user, wali, normalizeClassCode(req.ClassCode), student, session)
	if err != nil {
		return wrap(err)
	}

	// a new child changes the ranking so the cached leaderboard is stale
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	return nil
}

func (u *AuthUseCase) Login(ctx context.Context, req dto.LoginRequest) (dto.AuthResponse, error) {
	user, err := u.authenticate(ctx, req.Email, req.Password)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	if user.Role != req.Role {
		return dto.AuthResponse{}, apperror.ErrInvalidCredentials
	}

	// said only after the password was right, so the answer does not tell whether an email has an account
	if user.EmailVerificationPending && u.cfg.EmailVerificationRequired {
		return dto.AuthResponse{}, apperror.ErrEmailNotVerified
	}

	return u.startSession(ctx, user, req.DeviceToken)
}

// authenticate returns the account of the email when the secret matches its hash. A password that is wrong and an email that
// no account has give the same answer after the same work.
func (u *AuthUseCase) authenticate(ctx context.Context, email string, password string) (*entity.User, error) {
	// bcrypt only reads the first 72 bytes, so a longer password could match a shorter one that shares them
	if len([]byte(password)) > bcryptMaxBytes {
		return nil, apperror.ErrInvalidCredentials
	}

	user, err := u.repo.FindUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if user == nil {
		// the same work as for a wrong password, so the answer does not tell whether the email exists
		_, err = u.hasher.Matches(ctx, u.hasher.DummyHash(), password)
		if err != nil {
			return nil, apperror.As(err)
		}

		return nil, apperror.ErrInvalidCredentials
	}

	matches, err := u.hasher.Matches(ctx, user.PasswordHash, password)
	if err != nil {
		return nil, apperror.As(err)
	}

	if !matches {
		return nil, apperror.ErrInvalidCredentials
	}

	return user, nil
}

// VerifyEmail takes the code of the mail of a new account. The password has to match first, so a stranger who knows only the email
// cannot use up the tries of the code and keep the owner from verifying. A wrong, expired and used up code give the same answer.
func (u *AuthUseCase) VerifyEmail(ctx context.Context, req dto.VerifyEmailRequest) (dto.AuthResponse, error) {
	user, err := u.authenticate(ctx, req.Email, req.Password)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	if !user.EmailVerificationPending {
		return dto.AuthResponse{}, apperror.ErrEmailAlreadyVerified
	}

	matched, err := u.repo.CheckVerification(ctx, user.ID, u.codeHash(user.ID, req.Code), u.now(), constants.EmailCodeMaxAttempts)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	if !matched {
		return dto.AuthResponse{}, apperror.ErrVerificationCodeInvalid
	}

	user.EmailVerificationPending = false

	// the code was the proof, so the first session opens here, with the hash that was read before it
	return u.startSession(ctx, user, "")
}

// ResendVerification sends a new code to an account that waits for its email. The password has to match, so the route cannot be
// used to mail an address that the caller does not hold, and the cooldown and the hourly cap bound what one account can cost.
func (u *AuthUseCase) ResendVerification(ctx context.Context, req dto.ResendVerificationRequest) (dto.VerificationResponse, error) {
	user, err := u.authenticate(ctx, req.Email, req.Password)
	if err != nil {
		return dto.VerificationResponse{}, err
	}

	if !user.EmailVerificationPending {
		return dto.VerificationResponse{}, apperror.ErrEmailAlreadyVerified
	}

	sent, err := u.issueVerification(ctx, user)
	if err != nil {
		return dto.VerificationResponse{}, err
	}

	if !sent {
		return dto.VerificationResponse{}, apperror.ErrEmailSendFailed
	}

	return *verificationInfo(true), nil
}

// issueVerification stores a new code for the account and mails it. A code that is refused for the cooldown or the hourly cap
// is an error with the time to wait. A mail that did not go out is not an error, the result says so, because the code is stored
// and the caller decides what that means for its own answer.
func (u *AuthUseCase) issueVerification(ctx context.Context, user *entity.User) (bool, error) {
	sent, _, err := u.issue(ctx, user, false)

	return sent, err
}

// RetryVerification mails a new code to an account whose last mail failed. Every try is a new code, because the earlier one went
// nowhere, and a try that fails is scheduled again like any failed mail.
func (u *AuthUseCase) RetryVerification(ctx context.Context, userID uuid.UUID) error {
	user, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// an account that was verified, deleted or whose verification is off needs no code
	if user == nil || !user.EmailVerificationPending || !u.cfg.EmailVerificationRequired {
		return u.repo.ClearMailRetry(ctx, userID)
	}

	_, mailErr, err := u.issue(ctx, user, true)
	if errors.Is(err, repository.ErrNoRetry) {
		return nil
	}

	var refusal *apperror.Error
	if errors.As(err, &refusal) {
		switch refusal.Code {
		case apperror.ErrCodeLimitReached.Code, apperror.ErrInvalidCredentials.Code:
			// no more codes will ever be sent to the account, or it was removed or verified a moment ago
			return u.repo.ClearMailRetry(ctx, userID)
		case "VERIFICATION_RESEND_TOO_SOON":
			// the person asked for a code in the meantime, and the next try comes after the lease
			return nil
		}
	}

	if err != nil {
		return err
	}

	for _, refusal := range []error{mailer.ErrQuota, mailer.ErrRateLimit} {
		if errors.Is(mailErr, refusal) {
			return refusal
		}
	}

	return nil
}

// issue is issueVerification with the error of the mail service, which the caller may need to tell a quota from another failure
func (u *AuthUseCase) issue(ctx context.Context, user *entity.User, retry bool) (sent bool, mailErr error, err error) {
	code, err := newVerificationCode()
	if err != nil {
		return false, nil, apperror.Internal(err)
	}

	codeHash := u.codeHash(user.ID, code)

	wait, err := u.repo.StoreVerification(ctx, user.ID, codeHash, u.now(), repository.SendRule{
		TTL:       constants.EmailCodeTTLMinutes * time.Minute,
		Cooldown:  constants.EmailResendCooldownSeconds * time.Second,
		PerHour:   constants.EmailSendsPerHour,
		MaxTotal:  u.cfg.EmailCodesMaxPerAccount,
		OnlyRetry: retry,
	})
	if errors.Is(err, repository.ErrNoRetry) {
		return false, nil, err
	}
	if errors.Is(err, repository.ErrCodeLimit) {
		return false, nil, apperror.ErrCodeLimitReached
	}

	// the account was removed or verified after its password was checked
	if errors.Is(err, repository.ErrNotWaiting) {
		return false, nil, apperror.ErrInvalidCredentials
	}

	if err != nil {
		return false, nil, apperror.Internal(err)
	}

	if wait > 0 {
		return false, nil, apperror.ErrResendTooSoon(int((wait + time.Second - 1) / time.Second))
	}

	err = u.mail.SendVerificationCode(ctx, user.Email, code, constants.EmailCodeTTLMinutes)
	if err != nil {
		u.mails.WithLabelValues("failed").Inc()
		log.Printf("verification mail failed %v", err)
		u.report(fmt.Errorf("verification mail: %w", err))

		// nothing was mailed, so the wait and the send of the hour are given back and the person can ask again at once. The context
		// of the request may be the reason of the failure, so the release has its own.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()

		releaseErr := u.repo.ReleaseVerification(releaseCtx, user.ID, codeHash, u.now(), constants.EmailResendCooldownSeconds*time.Second)
		if releaseErr != nil {
			log.Printf("verification send was not given back %v", releaseErr)
		}

		// a mail that failed is tried again, except when no key is set, which no retry can mend. It has a context of its own, so a
		// slow release does not lose the retry.
		if !errors.Is(err, mailer.ErrDisabled) {
			markCtx, cancelMark := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
			defer cancelMark()

			markErr := u.repo.MarkMailFailed(markCtx, user.ID, codeHash, u.now())
			if markErr != nil {
				log.Printf("verification mail retry was not scheduled %v", markErr)
			}
		}

		return false, err, nil
	}

	u.mails.WithLabelValues("sent").Inc()

	// a mail went out, so the retries of an earlier failure are over
	clearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()

	clearErr := u.repo.ClearMailRetry(clearCtx, user.ID)
	if clearErr != nil {
		log.Printf("verification mail retries were not cleared %v", clearErr)
	}

	return true, nil, nil
}

// codeHash is what is stored for a code. A keyed hash of the account and the code means a copy of the table is no use without the
// secret of the server, and a code of one account never matches the row of another.
func (u *AuthUseCase) codeHash(userID uuid.UUID, code string) string {
	mac := hmac.New(sha256.New, []byte(u.cfg.JWTSecretKey))
	mac.Write([]byte("email-verification:" + userID.String() + ":" + code))

	return hex.EncodeToString(mac.Sum(nil))
}

// newVerificationCode returns a random code of EmailCodeDigits digits, leading zeros kept
func newVerificationCode() (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(constants.EmailCodeDigits), nil)

	number, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%0*d", constants.EmailCodeDigits, number), nil
}

func verificationInfo(sent bool) *dto.VerificationResponse {
	return &dto.VerificationResponse{
		Sent:               sent,
		ExpiresInSeconds:   constants.EmailCodeTTLMinutes * 60,
		ResendAfterSeconds: constants.EmailResendCooldownSeconds,
	}
}

// startSession opens a session and hands the device a token for the rate limiter. A device that sent a valid token
// for this email gets one with the same device id, so it keeps its own bucket.
func (u *AuthUseCase) startSession(ctx context.Context, user *entity.User, presented string) (dto.AuthResponse, error) {
	res, err := u.issueSession(ctx, user)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	return u.withDeviceToken(res, user, presented)
}

// withDeviceToken hands the device the token for the rate limiter of the email of the user
func (u *AuthUseCase) withDeviceToken(res dto.AuthResponse, user *entity.User, presented string) (dto.AuthResponse, error) {
	var err error

	res.DeviceToken, err = u.devices.Issue(user.Email, presented)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	return res, nil
}

// Refresh swaps a refresh token for a new session. Postgres alone decides whether the token is valid. A token that was used
// a moment ago, within the leeway, is answered with a new token of the same session, because the client that lost the answer
// to its first call retries with the old one. Later the same token ends the whole session.
func (u *AuthUseCase) Refresh(ctx context.Context, refreshToken string) (dto.AuthResponse, error) {
	hash := hashToken(refreshToken)

	raw, next := u.newRefreshToken()

	rotation, err := u.repo.RotateRefreshToken(ctx, hash, u.now(), next, repository.RotationPolicy{
		SessionMax:  time.Duration(u.cfg.JWTSessionMaxDays) * 24 * time.Hour,
		ReuseLeeway: time.Duration(u.cfg.RefreshReuseLeewaySeconds) * time.Second,
	})
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	if rotation.Replayed {
		log.Printf("a refresh token that was used a moment ago was shown again, user %s got a new token of the same session", rotation.User.ID)
	}

	if rotation.Reused {
		log.Printf("a refresh token that was already used was shown again, the session of user %s ended", rotation.UserID)
	}

	if rotation.User == nil {
		return dto.AuthResponse{}, apperror.ErrRefreshInvalid
	}

	return u.buildResponse(rotation.User, raw)
}

func (u *AuthUseCase) Logout(ctx context.Context, refreshToken string) error {
	hash := hashToken(refreshToken)

	err := u.repo.DeleteRefreshToken(ctx, hash)
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (u *AuthUseCase) issueSession(ctx context.Context, user *entity.User) (dto.AuthResponse, error) {
	raw, row := u.newRefreshToken()
	row.UserID = user.ID

	err := u.repo.CreateRefreshToken(ctx, row, user.PasswordHash, user.GoogleSub, time.Duration(u.cfg.JWTSessionMaxDays)*24*time.Hour)
	if errors.Is(err, repository.ErrCredentialsChanged) {
		return dto.AuthResponse{}, apperror.ErrInvalidCredentials
	}

	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	return u.buildResponse(user, raw)
}

func (u *AuthUseCase) buildResponse(user *entity.User, refreshToken string) (dto.AuthResponse, error) {
	token, err := u.jwt.GenerateToken(user.ID, user.Role)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	return dto.AuthResponse{Token: token, RefreshToken: refreshToken, User: userResponse(user)}, nil
}

func userResponse(user *entity.User) dto.UserResponse {
	return dto.UserResponse{
		ID:        user.ID,
		Email:     user.Email,
		FullName:  user.FullName,
		Role:      user.Role,
		AvatarURL: user.AvatarURL,
	}
}

// newRefreshToken returns the secret for the client and the row to store, which only holds its hash
func (u *AuthUseCase) newRefreshToken() (string, *entity.RefreshToken) {
	raw := uuid.NewString()
	now := u.now()

	// a sign in lasts for at most JWT_SESSION_MAX_DAYS however often its token is swapped
	lifetime := time.Duration(min(u.cfg.JWTRefreshExpiredDays, u.cfg.JWTSessionMaxDays)) * 24 * time.Hour

	return raw, &entity.RefreshToken{
		ID:               uuid.New(),
		TokenHash:        hashToken(raw),
		ExpiresAt:        now.Add(lifetime),
		CreatedAt:        now,
		FamilyID:         uuid.New(),
		SessionStartedAt: now,
	}
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}

func normalizeClassCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

// wrap keeps application errors intact and marks everything else as internal
func wrap(err error) error {
	if err == nil {
		return nil
	}

	return apperror.As(err)
}
