package auth

import (
	"fmt"
	"strings"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) error {
	if hash == "" || password == "" {
		return apperror.ErrUnauthenticated
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return fmt.Errorf("password mismatch: %w", apperror.ErrUnauthenticated)
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 10 || len(password) > 128 {
		return fmt.Errorf("password length: %w", apperror.ErrValidation)
	}
	var letter, digit bool
	for _, value := range password {
		switch {
		case value >= '0' && value <= '9':
			digit = true
		case strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", value):
			letter = true
		}
	}
	if !letter || !digit {
		return fmt.Errorf("password must contain a letter and digit: %w", apperror.ErrValidation)
	}
	return nil
}
