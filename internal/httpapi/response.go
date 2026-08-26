package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/audit"
)

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Default().Error("write JSON response", "error", err)
	}
}

func writeError(ctx context.Context, w http.ResponseWriter, err error) {
	status := statusFor(err)
	code := apperror.Code(err)
	message := publicMessage(code)
	body := errorBody{}
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = audit.RequestID(ctx)
	if status >= 500 {
		slog.Default().ErrorContext(ctx, "request failed", "request_id", body.Error.RequestID, "error", err)
	}
	writeJSON(w, status, body)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		return http.StatusInternalServerError
	case errors.Is(err, apperror.ErrValidation):
		return http.StatusBadRequest
	case errors.Is(err, apperror.ErrUnauthenticated), errors.Is(err, apperror.ErrExpired):
		return http.StatusUnauthorized
	case errors.Is(err, apperror.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, apperror.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, apperror.ErrConflict), errors.Is(err, apperror.ErrStaleVersion),
		errors.Is(err, apperror.ErrCapacity), errors.Is(err, apperror.ErrInvalidState):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func publicMessage(code string) string {
	switch code {
	case "validation_error":
		return "请求内容不符合业务规则"
	case "unauthenticated", "expired":
		return "登录状态无效或已过期"
	case "forbidden":
		return "当前身份无权执行此操作"
	case "not_found":
		return "请求的业务对象不存在"
	case "conflict":
		return "业务状态或资源已被其他操作改变"
	case "timeout":
		return "请求处理超时，请确认状态后重试"
	case "request_cancelled":
		return "请求已取消"
	default:
		return "服务暂时无法完成请求"
	}
}
