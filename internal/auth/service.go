package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vacks/fluxzy-console/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrDisabled           = errors.New("账号已被禁用")
	ErrTooManyAttempts    = errors.New("尝试次数过多，请稍后再试")
	ErrSetupComplete      = errors.New("管理员已经初始化")
	ErrValidation         = errors.New("参数不合法")
)

type LoginResult struct {
	User      *store.User
	Token     string
	CSRF      string
	ExpiresAt time.Time
}

type Service struct {
	store      *store.Store
	limiter    *Limiter
	sessionTTL time.Duration
	setupMu    sync.Mutex

	touchMu   sync.Mutex
	lastTouch map[int64]time.Time

	ipLimiter *Limiter
	kdfSem    chan struct{}
}

func NewService(st *store.Store) *Service {
	return &Service{
		store:      st,
		limiter:    NewLimiter(),
		ipLimiter:  NewLimiter(),
		kdfSem:     make(chan struct{}, 3),
		sessionTTL: 7 * 24 * time.Hour,
		lastTouch:  make(map[int64]time.Time),
	}
}

func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	return n == 0, err
}

func (s *Service) Setup(ctx context.Context, username, password string) (*store.User, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	required, err := s.SetupRequired(ctx)
	if err != nil {
		return nil, err
	}
	if !required {
		return nil, ErrSetupComplete
	}
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.store.CreateUser(ctx, username, hash)
}

func (s *Service) Login(ctx context.Context, username, password, ip, ua string) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	if ValidateUsername(username) != nil || ValidatePassword(password) != nil {
		return nil, ErrInvalidCredentials
	}
	key := ip + "|" + strings.ToLower(username)
	ipKey := "ip|" + ip
	if !s.ipLimiter.Allow(ipKey) || !s.limiter.Allow(key) {
		return nil, ErrTooManyAttempts
	}
	select {
	case s.kdfSem <- struct{}{}:
		defer func() { <-s.kdfSem }()
	default:
		return nil, ErrTooManyAttempts
	}

	fail := func() {
		s.limiter.Fail(key)
		s.ipLimiter.Fail(ipKey)
	}

	u, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_ = VerifyPassword(dummyHash, password)
			fail()
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if u.Disabled {
		_ = VerifyPassword(u.PasswordHash, password)
		fail()
		return nil, ErrInvalidCredentials
	}
	if !VerifyPassword(u.PasswordHash, password) {
		fail()
		return nil, ErrInvalidCredentials
	}
	s.limiter.Success(key)
	s.ipLimiter.Success(ipKey)

	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(s.sessionTTL)
	if err := s.store.CreateAuthSession(ctx, u.ID, hashToken(token), csrf, expires.UnixMilli(), ip, ua); err != nil {
		return nil, err
	}
	s.store.TouchLastLogin(ctx, u.ID)

	return &LoginResult{User: u, Token: token, CSRF: csrf, ExpiresAt: expires}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (*store.User, *store.AuthSession, error) {
	if token == "" {
		return nil, nil, store.ErrNotFound
	}
	as, u, err := s.store.GetAuthSession(ctx, hashToken(token))
	if err != nil {
		return nil, nil, err
	}
	if u.Disabled {
		return nil, nil, ErrDisabled
	}
	// last_seen_at is diagnostic only; writing it on every poll/asset request
	// would add unnecessary SQLite write contention. Throttle to once/minute.
	s.touchMu.Lock()
	last := s.lastTouch[as.ID]
	if time.Since(last) >= time.Minute {
		s.lastTouch[as.ID] = time.Now()
		s.touchMu.Unlock()
		s.store.TouchAuthSession(ctx, as.ID)
	} else {
		s.touchMu.Unlock()
	}
	return u, as, nil
}

func (s *Service) Logout(ctx context.Context, token string) {
	if token != "" {
		_ = s.store.DeleteAuthSession(ctx, hashToken(token))
	}
}

// PruneTouch drops throttling state for sessions that have not been seen for a
// while. Expired sessions are deleted hourly, but their map entries otherwise
// remain forever.
func (s *Service) PruneTouch(before time.Time) {
	s.touchMu.Lock()
	for id, seen := range s.lastTouch {
		if seen.Before(before) {
			delete(s.lastTouch, id)
		}
	}
	s.touchMu.Unlock()
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(u.PasswordHash, current) {
		return ErrInvalidCredentials
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.UpdateUserPassword(ctx, userID, hash); err != nil {
		return err
	}
	return s.store.DeleteUserAuthSessions(ctx, userID)
}

func ValidateUsername(v string) error {
	if len(v) < 3 || len(v) > 64 {
		return fmt.Errorf("%w: 用户名长度需为 3-64 个字符", ErrValidation)
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return fmt.Errorf("%w: 用户名只能包含字母、数字、_、-、.", ErrValidation)
	}
	return nil
}

func ValidatePassword(v string) error {
	if len(v) < 5 || len(v) > 200 {
		return fmt.Errorf("%w: 密码长度需为 5-200 个字符", ErrValidation)
	}
	return nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
