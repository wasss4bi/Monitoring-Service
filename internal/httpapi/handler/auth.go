package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"monitoring_service/internal/authjwt"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/logging"
	"monitoring_service/internal/service"
)

const accessTokenLifetime = 24 * time.Hour

type authenticatedUserContextKey struct{}
type authenticatedVersionContextKey struct{}

type AuthHandler struct {
	users     *service.UserService
	jwtSecret string
	logger    *slog.Logger
}

func NewAuthHandler(users *service.UserService, jwtSecret string, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{users: users, jwtSecret: jwtSecret, logger: logger}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.login(w, r)
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if !decodeJSON(w, r, &input) {
		return
	}

	user, err := h.users.Authenticate(r.Context(), domain.LoginInput{
		Email:    input.Email,
		Password: input.Password,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	token, err := authjwt.Generate(h.jwtSecret, authjwt.Claims{
		UserID:      user.ID,
		AuthVersion: user.AuthVersion,
		ExpiresAt:   time.Now().Add(accessTokenLifetime).Unix(),
	})
	if err != nil {
		logging.Error(r.Context(), h.logger, "failed to issue JWT access token", map[string]any{
			"user_id":               user.ID,
			"jwt_secret_configured": strings.TrimSpace(h.jwtSecret) != "",
		}, err)
		setRequestError(w, err)
		writeError(w, http.StatusInternalServerError, "could not issue an access token; try again later")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{AccessToken: token, TokenType: "Bearer"})
}

func AuthenticateJWT(jwtSecret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeUnauthorized(w)
			return
		}

		claims, err := authjwt.Parse(jwtSecret, parts[1])
		if err != nil {
			writeUnauthorized(w)
			return
		}
		ctx := context.WithValue(r.Context(), authenticatedUserContextKey{}, claims.UserID)
		ctx = context.WithValue(ctx, authenticatedVersionContextKey{}, claims.AuthVersion)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authenticatedUserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(authenticatedUserContextKey{}).(int64)
	return id, ok && id > 0
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "authentication required")
}
