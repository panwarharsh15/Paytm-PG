package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// APIResponse is the standard response envelope for all API calls.
// Consistent shape makes frontend development much easier.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func respond(c *gin.Context, statusCode int, success bool, message string, data interface{}, errMsg string) {
	c.JSON(statusCode, APIResponse{
		Success: success,
		Message: message,
		Data:    data,
		Error:   errMsg,
	})
}

// ─── Success responses ───────────────────────────────────────────────────────

func OK(c *gin.Context, data interface{}) {
	respond(c, http.StatusOK, true, "", data, "")
}

func Created(c *gin.Context, data interface{}) {
	respond(c, http.StatusCreated, true, "created successfully", data, "")
}

// ─── Error responses ─────────────────────────────────────────────────────────

func ValidationError(c *gin.Context, err error) {
	respond(c, http.StatusBadRequest, false, "", nil, err.Error())
}

func BadRequestError(c *gin.Context, msg string) {
	respond(c, http.StatusBadRequest, false, "", nil, msg)
}

func UnauthorizedError(c *gin.Context, msg string) {
	respond(c, http.StatusUnauthorized, false, "", nil, msg)
}

func ForbiddenError(c *gin.Context, msg string) {
	respond(c, http.StatusForbidden, false, "", nil, msg)
}

func NotFoundError(c *gin.Context, msg string) {
	respond(c, http.StatusNotFound, false, "", nil, msg)
}

func ConflictError(c *gin.Context, msg string) {
	respond(c, http.StatusConflict, false, "", nil, msg)
}

func InternalError(c *gin.Context, err error) {
	// Never expose internal errors to the client
	respond(c, http.StatusInternalServerError, false, "", nil, "internal server error")
}

// ─── Context helpers ─────────────────────────────────────────────────────────

// MustGetUserID reads the authenticated user's ID from Gin context.
// Panics if not present — middleware should always set this.
func MustGetUserID(c *gin.Context) uuid.UUID {
	userID, exists := c.Get("user_id")
	if !exists {
		panic("user_id not found in context — auth middleware not applied")
	}
	return userID.(uuid.UUID)
}

func GetUserRole(c *gin.Context) string {
	role, _ := c.Get("user_role")
	if r, ok := role.(string); ok {
		return r
	}
	return ""
}
