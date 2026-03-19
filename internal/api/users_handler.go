// internal/api/users_handler.go
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// UserResponse is the user JSON structure returned by the API.
type UserResponse struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UsersHandler handles /api/users routes.
type UsersHandler struct {
	db *database.Database
}

// NewUsersHandler constructs a UsersHandler.
func NewUsersHandler(db *database.Database) *UsersHandler {
	return &UsersHandler{db: db}
}

// List handles GET /api/users
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.DB().QueryContext(r.Context(),
		`SELECT id, username, email, is_admin, created_at, updated_at FROM users ORDER BY id ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query users")
		return
	}
	defer rows.Close()

	users := make([]UserResponse, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan user")
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	JSON(w, http.StatusOK, users)
}

// Create handles POST /api/users
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.Username) == "" {
		Error(w, http.StatusBadRequest, "username is required")
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		Error(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(req.Password) < 8 {
		Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	isAdminInt := 0
	if req.IsAdmin {
		isAdminInt = 1
	}

	res, err := h.db.DB().ExecContext(r.Context(),
		`INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)`,
		req.Username, req.Email, hash, isAdminInt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			Error(w, http.StatusConflict, "username or email already exists")
			return
		}
		Error(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	id, _ := res.LastInsertId()
	user, ok := h.fetchUser(r, int(id))
	if !ok {
		Error(w, http.StatusInternalServerError, "failed to retrieve created user")
		return
	}

	JSON(w, http.StatusCreated, user)
}

// Update handles PUT /api/users/{id}
// Allows changing password, email, and is_admin.
func (h *UsersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  *bool  `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Check user exists
	var exists int
	if err := h.db.DB().QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM users WHERE id=?", id,
	).Scan(&exists); err != nil || exists == 0 {
		Error(w, http.StatusNotFound, "user not found")
		return
	}

	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}

	if req.Email != "" {
		sets = append(sets, "email = ?")
		args = append(args, req.Email)
	}

	if req.Password != "" {
		if len(req.Password) < 8 {
			Error(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			Error(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		sets = append(sets, "password_hash = ?")
		args = append(args, hash)
	}

	if req.IsAdmin != nil {
		isAdminInt := 0
		if *req.IsAdmin {
			isAdminInt = 1
		}
		sets = append(sets, "is_admin = ?")
		args = append(args, isAdminInt)
	}

	args = append(args, id)
	query := "UPDATE users SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := h.db.DB().ExecContext(r.Context(), query, args...); err != nil {
		Error(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	user, found := h.fetchUser(r, id)
	if !found {
		Error(w, http.StatusInternalServerError, "failed to retrieve updated user")
		return
	}

	JSON(w, http.StatusOK, user)
}

// Delete handles DELETE /api/users/{id}
func (h *UsersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Prevent deleting the last admin
	var adminCount int
	h.db.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE is_admin=1").Scan(&adminCount)

	var isAdmin int
	h.db.DB().QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id=?", id).Scan(&isAdmin)

	if isAdmin == 1 && adminCount <= 1 {
		Error(w, http.StatusBadRequest, "cannot delete the last admin user")
		return
	}

	res, err := h.db.DB().ExecContext(r.Context(), "DELETE FROM users WHERE id=?", id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		Error(w, http.StatusNotFound, "user not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func (h *UsersHandler) fetchUser(r *http.Request, id int) (UserResponse, bool) {
	row := h.db.DB().QueryRowContext(r.Context(),
		`SELECT id, username, email, is_admin, created_at, updated_at FROM users WHERE id=?`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return UserResponse{}, false
	}
	if err != nil {
		return UserResponse{}, false
	}
	return u, true
}

func scanUser(row interface{ Scan(...interface{}) error }) (UserResponse, error) {
	var u UserResponse
	var isAdminInt int
	var createdAt, updatedAt string

	err := row.Scan(&u.ID, &u.Username, &u.Email, &isAdminInt, &createdAt, &updatedAt)
	if err != nil {
		return UserResponse{}, err
	}

	u.IsAdmin = isAdminInt != 0
	u.CreatedAt = parseTime(createdAt)
	u.UpdatedAt = parseTime(updatedAt)
	return u, nil
}
