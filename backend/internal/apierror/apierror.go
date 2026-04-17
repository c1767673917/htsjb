package apierror

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Wrap(err error, status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message, Err: err}
}

func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

var (
	ErrBadRequest           = New(http.StatusBadRequest, "BAD_REQUEST", "请求不合法")
	ErrNoStagedFiles        = New(http.StatusBadRequest, "ORDER_HAS_NO_STAGED_FILES", "请先添加至少一张图片")
	ErrUnauthenticated      = New(http.StatusUnauthorized, "UNAUTHENTICATED", "未登录或登录已失效")
	ErrYearNotFound         = New(http.StatusNotFound, "YEAR_NOT_FOUND", "年份不存在")
	ErrOrderNotFound        = New(http.StatusNotFound, "ORDER_NOT_FOUND", "订单不存在")
	ErrFileNotFound         = New(http.StatusNotFound, "FILE_NOT_FOUND", "文件不存在")
	ErrUploadCapExceeded    = New(http.StatusConflict, "UPLOAD_CAP_EXCEEDED", "单种类型最多上传 50 张图片")
	ErrRequestTooLarge      = New(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "上传内容过大")
	ErrUnsupportedMediaType = New(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "仅支持 JPEG、PNG、WebP 图片")
	ErrOrderLocked          = New(http.StatusLocked, "ORDER_LOCKED", "当前订单正在被其他操作处理")
	ErrNotPhotoOwner        = New(http.StatusForbidden, "NOT_PHOTO_OWNER", "仅能删除本人上传的图片")
	ErrRateLimited          = New(http.StatusTooManyRequests, "RATE_LIMITED", "登录尝试过于频繁，请稍后再试")
	ErrServerBusy           = New(http.StatusServiceUnavailable, "SERVER_BUSY", "服务器繁忙，请稍后重试")
	ErrInternal             = New(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
)
