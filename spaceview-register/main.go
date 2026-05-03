package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anyproto/any-sync/util/crypto"
	"github.com/anyproto/anytype-heart/core/anytype"
	heartconfig "github.com/anyproto/anytype-heart/core/anytype/config"
	"github.com/anyproto/anytype-heart/core/event"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/space"
	"github.com/anyproto/anytype-heart/space/spaceinfo"
	"golang.org/x/term"
)

const (
	dataRoot = "[hier stond de nieuwe data-root]"
	account = "[hier stond het nieuwe vault id]"
	spaceID = "[hier stond de space id]"
	aclHead = "[hier stond de acl head]"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := ensureAnytypeClosed(); err != nil {
		return err
	}
	mnemonic, err := readSecret("New mnemonic")
	if err != nil {
		return err
	}
	derived, err := crypto.Mnemonic(mnemonic).DeriveKeys(0)
	if err != nil {
		return err
	}
	if got := derived.Identity.GetPublic().Account(); got != account {
		return fmt.Errorf("new mnemonic derived %s, expected %s", got, account)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, account, "spaceStoreNew", spaceID, "store.db")); err != nil {
		return fmt.Errorf("target space store missing: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cfg := anytype.BootstrapConfig(false, "")
	heartconfig.WithDisabledLocalNetworkSync()(cfg)
	w := anytype.BootstrapWallet(dataRoot, derived, "en")
	app, err := anytype.StartNewApp(ctx, "local-spaceview-register", event.NewCallbackSender(func(*pb.Event) {}), cfg, w)
	if err != nil {
		return fmt.Errorf("start Heart app: %w", err)
	}
	defer app.Close(context.Background())

	spaceService := app.MustComponent(space.CName).(space.Service)
	if _, err := spaceService.GetTechSpace(ctx); err != nil {
		return fmt.Errorf("load techspace: %w", err)
	}
	info := spaceinfo.NewSpacePersistentInfo(spaceID)
	info.SetAccountStatus(spaceinfo.AccountStatusActive).SetAclHeadId(aclHead)
	err = spaceService.TechSpace().SpaceViewCreate(ctx, spaceID, false, info, nil)
	if err != nil {
		return fmt.Errorf("create space view: %w", err)
	}
	fmt.Println("space view registered")
	return nil
}

func readSecret(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("%s is empty", strings.ToLower(label))
	}
	return s, nil
}

func ensureAnytypeClosed() error {
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		cmd := strings.ToLower(strings.ReplaceAll(string(cmdline), "\x00", " "))
		if strings.Contains(cmd, "anytype") {
			return fmt.Errorf("Anytype process appears to be running: %s", strings.TrimSpace(cmd))
		}
	}
	return nil
}
