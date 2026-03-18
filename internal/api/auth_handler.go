// internal/api/auth_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// AuthHandler handles authentication-related HTTP endpoints.
type AuthHandler struct {
	db      *database.Database
	authSvc *auth.Service
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(db *database.Database, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{db: db, authSvc: authSvc}
}

// loginRequest is the JSON body expected by Login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login handles POST /api/auth/login.
// It looks up the user by username, checks the password, generates a JWT,
// and sets an httpOnly SameSite=Strict cookie.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		Error(w, http.StatusBadRequest, "username and password required")
		return
	}

	// Look up user by username
	var (
		userID       int
		passwordHash string
		email        string
		isAdmin      bool
		isAdminInt   int
	)
	err := h.db.DB().QueryRow(
		"SELECT id, email, password_hash, is_admin FROM users WHERE username = ?",
		req.Username,
	).Scan(&userID, &email, &passwordHash, &isAdminInt)
	if err == sql.ErrNoRows {
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	isAdmin = isAdminInt != 0

	// Verify password
	if !auth.CheckPassword(req.Password, passwordHash) {
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Generate JWT
	token, err := h.authSvc.GenerateToken(userID, req.Username, isAdmin)
	if err != nil {
		Error(w, http.StatusInternalServerError, "could not generate token")
		return
	}

	// Set secure cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   86400, // 24 hours
	})

	JSON(w, http.StatusOK, map[string]interface{}{
		"user_id":  userID,
		"username": req.Username,
		"email":    email,
		"is_admin": isAdmin,
	})
}

// Logout handles POST /api/auth/logout.
// It clears the token cookie by setting MaxAge=-1.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   -1,
	})
	JSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// resetPasswordRequest is the JSON body expected by ResetPassword.
type resetPasswordRequest struct {
	Email string `json:"email"`
}

// ResetPassword handles POST /api/auth/reset-password.
// Generates a reset token and stores its SHA256 hash in the settings table.
// Always returns 200 to prevent email enumeration.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Still return 200 — no enumeration
		JSON(w, http.StatusOK, map[string]string{"message": "if that email exists, a reset link has been sent"})
		return
	}

	// Look up user by email
	var userID int
	err := h.db.DB().QueryRow(
		"SELECT id FROM users WHERE email = ?",
		req.Email,
	).Scan(&userID)
	if err != nil {
		// User not found or DB error — return 200 anyway (no enumeration)
		JSON(w, http.StatusOK, map[string]string{"message": "if that email exists, a reset link has been sent"})
		return
	}

	// Generate token
	rawToken, err := auth.GenerateResetToken()
	if err != nil {
		Error(w, http.StatusInternalServerError, "could not generate reset token")
		return
	}

	hash := auth.HashResetToken(rawToken)
	expiry := time.Now().Add(1 * time.Hour).Unix()
	settingVal := fmt.Sprintf("%d:%d", userID, expiry)

	_, err = h.db.DB().Exec(
		"INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		"reset_token:"+hash, settingVal,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "could not store reset token")
		return
	}

	// Log the token since email service isn't built yet
	log.Printf("[password-reset] token for user %d: %s", userID, rawToken)

	JSON(w, http.StatusOK, map[string]string{"message": "if that email exists, a reset link has been sent"})
}

// confirmResetRequest is the JSON body expected by ConfirmReset.
type confirmResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ConfirmReset handles POST /api/auth/reset-password/confirm.
// Looks up the token hash in settings, checks expiry, updates the user password,
// then deletes the token entry.
func (h *AuthHandler) ConfirmReset(w http.ResponseWriter, r *http.Request) {
	var req confirmResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Token == "" || req.NewPassword == "" {
		Error(w, http.StatusBadRequest, "token and new_password required")
		return
	}

	hash := auth.HashResetToken(req.Token)
	key := "reset_token:" + hash

	// Look up the token entry in settings
	var settingVal string
	err := h.db.DB().QueryRow(
		"SELECT value FROM settings WHERE key = ?", key,
	).Scan(&settingVal)
	if err == sql.ErrNoRows {
		Error(w, http.StatusBadRequest, "invalid or expired token")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Parse "userID:expiryUnix"
	parts := strings.SplitN(settingVal, ":", 2)
	if len(parts) != 2 {
		Error(w, http.StatusInternalServerError, "malformed token entry")
		return
	}
	userID, err := strconv.Atoi(parts[0])
	if err != nil {
		Error(w, http.StatusInternalServerError, "malformed token entry")
		return
	}
	expiryUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		Error(w, http.StatusInternalServerError, "malformed token entry")
		return
	}

	// Check expiry
	if time.Now().Unix() > expiryUnix {
		// Clean up expired token
		h.db.DB().Exec("DELETE FROM settings WHERE key = ?", key)
		Error(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	// Hash new password
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		Error(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	// Update user password
	_, err = h.db.DB().Exec(
		"UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		newHash, userID,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, "could not update password")
		return
	}

	// Delete token
	h.db.DB().Exec("DELETE FROM settings WHERE key = ?", key)

	JSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}
