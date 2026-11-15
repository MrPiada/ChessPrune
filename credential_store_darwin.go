//go:build darwin

package main

import (
	"errors"
	"os/exec"
	"strings"
)

const keychainService = "com.chessprune.lichess-token"

type macKeychainStore struct{}

func newTokenStore() tokenStore              { return macKeychainStore{} }
func (macKeychainStore) Description() string { return "macOS Keychain" }
func (macKeychainStore) Load() (string, error) {
	data, err := exec.Command("security", "find-generic-password", "-a", "ChessPrune", "-s", keychainService, "-w").Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 44 {
			return "", nil
		}
		return "", errors.New("could not read ChessPrune token from macOS Keychain")
	}
	return strings.TrimSpace(string(data)), nil
}
func (macKeychainStore) Save(token string) error {
	if err := validateLichessToken(token); err != nil {
		return err
	}
	// macOS security(1) accepts the password through -w, so it can be briefly
	// visible in the process argv to local process monitors during saving.
	if err := exec.Command("security", "add-generic-password", "-a", "ChessPrune", "-s", keychainService, "-w", token, "-U").Run(); err != nil {
		return errors.New("could not save token to macOS Keychain; check Keychain permissions")
	}
	return nil
}
func (macKeychainStore) Delete() error {
	if err := exec.Command("security", "delete-generic-password", "-a", "ChessPrune", "-s", keychainService).Run(); err != nil {
		if value, loadErr := (macKeychainStore{}).Load(); loadErr == nil && value == "" {
			return nil
		}
		return errors.New("could not delete ChessPrune token from macOS Keychain")
	}
	return nil
}
