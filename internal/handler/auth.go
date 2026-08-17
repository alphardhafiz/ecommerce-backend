package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"

	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

const (
	refreshCookieName   = "refresh_token"
	csrfCookieName      = "csrf_token"
	csrfHeaderName      = "X-CSRF-Token"
	refreshCookieMaxAge = 7 * 24 * 60 * 60 // 7 days, seconds
)

type Auth struct {
	svc *service.AuthService
}

func NewAuth(svc *service.AuthService) *Auth {
	return &Auth{svc: svc}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	result, err := a.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			respondError(w, http.StatusUnauthorized, "Invalid email or password", "INVALID_CREDENTIALS", nil)
		case errors.Is(err, service.ErrInactiveAccount):
			respondError(w, http.StatusForbidden, "Account is inactive", "ACCOUNT_INACTIVE", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	setSessionCookies(w, result.RefreshToken)
	respondJSON(w, http.StatusOK, loginResponse(result))
}

// setSessionCookies sets the httpOnly refresh cookie (for the backend) and a
// JS-readable CSRF cookie (for double-submit CSRF protection on /auth/refresh).
func setSessionCookies(w http.ResponseWriter, refreshToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    refreshToken,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   refreshCookieMaxAge,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    randomToken(),
		Path:     "/auth",
		HttpOnly: false, // frontend JS must read it to echo in X-CSRF-Token
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   refreshCookieMaxAge,
	})
}

func loginResponse(result *service.LoginResult) map[string]any {
	return map[string]any{
		"success": true,
		"data": map[string]any{
			"access_token": result.AccessToken,
			"expires_in":   result.ExpiresIn,
			"user": map[string]any{
				"id":   result.User.ID,
				"name": result.User.Name,
				"role": result.User.Role,
			},
		},
	}
}

func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request) {
	refresh, err := r.Cookie(refreshCookieName)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Invalid or expired refresh token", "INVALID_REFRESH_TOKEN", nil)
		return
	}

	csrfCookie, err := r.Cookie(csrfCookieName)
	csrfHeader := r.Header.Get(csrfHeaderName)
	if err != nil || !constantTimeEqual(csrfCookie.Value, csrfHeader) {
		respondError(w, http.StatusForbidden, "CSRF token mismatch", "CSRF_INVALID", nil)
		return
	}

	result, err := a.svc.Refresh(r.Context(), refresh.Value)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRefreshToken):
			respondError(w, http.StatusUnauthorized, "Invalid or expired refresh token", "INVALID_REFRESH_TOKEN", nil)
		case errors.Is(err, service.ErrInactiveAccount):
			respondError(w, http.StatusForbidden, "Account is inactive", "ACCOUNT_INACTIVE", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	setSessionCookies(w, result.RefreshToken)
	respondJSON(w, http.StatusOK, loginResponse(result))
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	// CSRF double-submit (same as /auth/refresh): cookie-endpoint protection.
	csrfCookie, err := r.Cookie(csrfCookieName)
	csrfHeader := r.Header.Get(csrfHeaderName)
	if err != nil || !constantTimeEqual(csrfCookie.Value, csrfHeader) {
		respondError(w, http.StatusForbidden, "CSRF token mismatch", "CSRF_INVALID", nil)
		return
	}

	if refresh, err := r.Cookie(refreshCookieName); err == nil {
		if err := a.svc.Logout(r.Context(), refresh.Value); err != nil {
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
			return
		}
	}

	clearAuthCookies(w)
	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{}})
}

func clearAuthCookies(w http.ResponseWriter) {
	for _, name := range []string{refreshCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/auth",
			HttpOnly: name == refreshCookieName,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
	}
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

func (a *Auth) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", []map[string]string{{"field": "email", "message": "Email is not valid"}})
		return
	}

	if err := a.svc.ForgotPassword(r.Context(), req.Email); err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"message": "If the email is registered, a reset link has been sent",
		},
	})
}

type resetPasswordRequest struct {
	Token           string `json:"token"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (a *Auth) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	err := a.svc.ResetPassword(r.Context(), req.Token, req.Password, req.ConfirmPassword)
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
			return
		}
		if errors.Is(err, service.ErrInvalidResetToken) {
			respondError(w, http.StatusBadRequest, "Invalid or expired reset token", "INVALID_RESET_TOKEN", nil)
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{},
	})
}

type registerRequest struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	user, err := a.svc.Register(r.Context(), req.Name, req.Email, req.Password, req.ConfirmPassword)
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
			return
		}
		if errors.Is(err, repository.ErrEmailTaken) {
			respondError(w, http.StatusConflict, "Email already exists", "EMAIL_ALREADY_EXISTS", nil)
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data": map[string]any{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

func respondError(w http.ResponseWriter, status int, message, code string, errorsList any) {
	if errorsList == nil {
		errorsList = []any{}
	}
	respondJSON(w, status, map[string]any{
		"success": false,
		"message": message,
		"code":    code,
		"errors":  errorsList,
	})
}
