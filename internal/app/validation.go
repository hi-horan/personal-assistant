package app

import (
	"unicode/utf8"

	"personal-assistant/internal/apperr"
)

const (
	maxAppNameLength = 256
	maxUserIDLength  = 50
	maxTitleLength   = 100
	maxKindLength    = 20
)

func validateScope(appName, userID string) error {
	if appName == "" {
		return apperr.New(apperr.CodeInvalid, "app_name is required")
	}
	if utf8.RuneCountInString(appName) > maxAppNameLength {
		return apperr.New(apperr.CodeInvalid, "app_name must be at most 256 characters")
	}
	if userID == "" {
		return apperr.New(apperr.CodeInvalid, "user_id is required")
	}
	if utf8.RuneCountInString(userID) > maxUserIDLength {
		return apperr.New(apperr.CodeInvalid, "user_id must be at most 50 characters")
	}
	return nil
}

func validateTitle(title string) error {
	if utf8.RuneCountInString(title) > maxTitleLength {
		return apperr.New(apperr.CodeInvalid, "title must be at most 100 characters")
	}
	return nil
}

func validateKind(kind string) error {
	if utf8.RuneCountInString(kind) > maxKindLength {
		return apperr.New(apperr.CodeInvalid, "kind must be at most 20 characters")
	}
	return nil
}
