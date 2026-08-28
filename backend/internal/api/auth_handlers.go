package api

import (
	"net/http"
	"strconv"
	"time"

	"step-ca-webui/internal/auth"
	"step-ca-webui/internal/db"

	"github.com/gin-gonic/gin"
)

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type UserResponse struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Role      db.Role   `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func toUserResponse(u db.User) UserResponse {
	return UserResponse{ID: u.ID, Username: u.Username, Role: u.Role, CreatedAt: u.CreatedAt}
}

// actorName returns the logged-in username for audit/ownership logging, or
// "system" as a defensive fallback -- every route that reaches this should
// already be behind RequireSession, so CurrentUser should never actually be
// nil here, but a fallback beats a nil-pointer panic if that ever changes.
func actorName(c *gin.Context) string {
	if u := CurrentUser(c); u != nil {
		return u.Username
	}
	return "system"
}

// Login checks the submitted credentials and, on success, creates a
// session and sets its cookie. Deliberately returns the same error message
// whether the username doesn't exist or the password is wrong (so a login
// attempt can't be used to enumerate valid usernames), and always runs a
// real bcrypt comparison even for an unknown username (against
// auth.DummyPasswordHash) so the response takes the same time either way.
func (h *Handlers) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	user, err := h.db.GetUserByUsername(req.Username)
	if err != nil {
		auth.CheckPassword(auth.DummyPasswordHash, req.Password)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	token, err := auth.GenerateSessionToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	session := &db.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(SessionDuration),
		CreatedAt: time.Now(),
	}
	if err := h.db.CreateSession(session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	// domain "" = host-only cookie bound to whatever host issued it, which
	// is what lets this same cookie flow to both the frontend's origin and
	// the backend's own port -- see the CORS comment in middleware.go for
	// the other half of making that work. secure=false: see SessionCookieName.
	c.SetCookie(SessionCookieName, token, int(SessionDuration.Seconds()), "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"username": user.Username, "role": user.Role})
}

// Logout deletes the session (if any) and clears the cookie. Safe to call
// with no session at all -- it just no-ops in that case, which is simpler
// than requiring RequireSession first only to immediately tear down what
// it just verified.
func (h *Handlers) Logout(c *gin.Context) {
	if token, err := c.Cookie(SessionCookieName); err == nil && token != "" {
		_ = h.db.DeleteSession(token)
	}
	c.SetCookie(SessionCookieName, "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// Me reports who's currently logged in -- what the frontend's AuthGate
// calls on every page load to decide between "show the app" and "redirect
// to /login," and also how it knows the current role for hiding
// admin-only UI from viewer accounts.
func (h *Handlers) Me(c *gin.Context) {
	user := CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not logged in"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"username": user.Username, "role": user.Role})
}

// ListUsers is admin-only (see routes.go) -- lets an admin see and manage
// every account from Settings instead of being stuck editing environment
// variables after the very first bootstrap account exists.
func (h *Handlers) ListUsers(c *gin.Context) {
	users, err := h.db.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	responses := make([]UserResponse, 0, len(users))
	for _, u := range users {
		responses = append(responses, toUserResponse(u))
	}
	c.JSON(http.StatusOK, gin.H{"users": responses})
}

type CreateUserRequest struct {
	Username string  `json:"username" binding:"required"`
	Password string  `json:"password" binding:"required"`
	Role     db.Role `json:"role" binding:"required"`
}

func (h *Handlers) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role != db.RoleAdmin && req.Role != db.RoleViewer {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin or viewer"})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user := &db.User{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
	}
	if err := h.db.CreateUser(user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": toUserResponse(*user)})
}

type UpdateUserRequest struct {
	Password string  `json:"password,omitempty"`
	Role     db.Role `json:"role,omitempty"`
}

func (h *Handlers) UpdateUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	user, err := h.db.GetUserByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Role != "" && req.Role != user.Role {
		if req.Role != db.RoleAdmin && req.Role != db.RoleViewer {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin or viewer"})
			return
		}
		// Don't allow demoting the last remaining admin -- that would lock
		// everyone out of both user management and every mutating
		// certificate operation, with no way back in except direct
		// database surgery.
		if user.Role == db.RoleAdmin {
			admins, err := h.db.CountUsersByRole(db.RoleAdmin)
			if err == nil && admins <= 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote the last remaining admin"})
				return
			}
		}
		user.Role = req.Role
	}

	if req.Password != "" {
		if len(req.Password) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		user.PasswordHash = hash
		// A password change should shut out any session created with the
		// old password -- otherwise a leaked/stolen session would survive
		// the very change meant to invalidate it.
		_ = h.db.DeleteSessionsForUser(user.ID)
	}

	if err := h.db.UpdateUser(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": toUserResponse(*user)})
}

func (h *Handlers) DeleteUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	user, err := h.db.GetUserByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if requester := CurrentUser(c); requester != nil && requester.ID == user.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete your own account while logged in as it"})
		return
	}

	if user.Role == db.RoleAdmin {
		admins, err := h.db.CountUsersByRole(db.RoleAdmin)
		if err == nil && admins <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete the last remaining admin"})
			return
		}
	}

	if err := h.db.DeleteUser(user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}
	_ = h.db.DeleteSessionsForUser(user.ID)

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}
