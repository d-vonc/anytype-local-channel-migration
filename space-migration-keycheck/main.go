package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/anyproto/any-sync/util/crypto"
	"golang.org/x/term"
)

const (
	expectedOldAccount = "[hier stond het oude vault id]"
	expectedNewAccount = "[hier stond het nieuwe vault id]"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	oldMnemonic, err := readSecret("Old mnemonic")
	if err != nil {
		return err
	}
	newMnemonic, err := readSecret("New mnemonic")
	if err != nil {
		return err
	}

	oldAccount, err := accountFromMnemonic(oldMnemonic)
	if err != nil {
		return fmt.Errorf("old mnemonic: %w", err)
	}
	newAccount, err := accountFromMnemonic(newMnemonic)
	if err != nil {
		return fmt.Errorf("new mnemonic: %w", err)
	}

	fmt.Printf("old account: %s\n", status(oldAccount, expectedOldAccount))
	fmt.Printf("new account: %s\n", status(newAccount, expectedNewAccount))

	if oldAccount != expectedOldAccount {
		return fmt.Errorf("old mnemonic derived %s, expected %s", oldAccount, expectedOldAccount)
	}
	if newAccount != expectedNewAccount {
		return fmt.Errorf("new mnemonic derived %s, expected %s", newAccount, expectedNewAccount)
	}

	fmt.Println("mnemonics match the expected vault accounts")
	return nil
}

func readSecret(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}
	secret := strings.TrimSpace(string(b))
	if secret == "" {
		return "", fmt.Errorf("%s is empty", strings.ToLower(label))
	}
	return secret, nil
}

func accountFromMnemonic(mnemonic string) (string, error) {
	derived, err := crypto.Mnemonic(mnemonic).DeriveKeys(0)
	if err != nil {
		return "", err
	}
	return derived.Identity.GetPublic().Account(), nil
}

func status(actual, expected string) string {
	if actual == expected {
		return expected + " (match)"
	}
	return actual + " (mismatch)"
}
