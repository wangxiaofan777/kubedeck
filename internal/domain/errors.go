package domain

import "errors"

// 仓储层的通用错误，handler 负责映射为统一错误码。
var (
	ErrNotFound = errors.New("domain: not found")
	ErrConflict = errors.New("domain: conflict")
)
