package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	anystore "github.com/anyproto/any-store"
	"github.com/anyproto/any-sync/commonspace/object/accountdata"
	"github.com/anyproto/any-sync/commonspace/object/acl/list"
	"github.com/anyproto/any-sync/commonspace/object/acl/recordverifier"
	"github.com/anyproto/any-sync/commonspace/object/tree/objecttree"
	"github.com/anyproto/any-sync/commonspace/object/tree/treechangeproto"
	"github.com/anyproto/any-sync/commonspace/object/tree/treestorage"
	"github.com/anyproto/any-sync/commonspace/spacestorage"
	"github.com/anyproto/any-sync/util/crypto"
	"github.com/anyproto/anytype-heart/core/domain"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/space/spacedomain"
	"golang.org/x/term"
)

const (
	dataRoot = "[hier stond de nieuwe data-root]"
	account = "[hier stond het nieuwe vault id]"
	techID  = "bafyreihxm3iit5dcswndph7ury4rhhjdkt5aihjlyefpiijt32jzvwep4q.2h2bi3kcvc3kt"
	spaceID = "[hier stond de space id]"
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

	ctx := context.Background()
	dbPath := filepath.Join(dataRoot, account, "spaceStoreNew", techID, "store.db")
	db, err := anystore.Open(ctx, dbPath, nil)
	if err != nil {
		return err
	}
	defer db.Close()
	st, err := spacestorage.New(ctx, techID, db)
	if err != nil {
		return err
	}
	aclStorage, err := st.AclStorage()
	if err != nil {
		return err
	}
	peerKey, _, err := crypto.GenerateRandomEd25519KeyPair()
	if err != nil {
		return err
	}
	acl, err := list.BuildAclListWithIdentity(accountdata.New(peerKey, derived.Identity), aclStorage, recordverifier.NewValidateFull())
	if err != nil {
		return err
	}
	defer acl.Close(ctx)

	key, err := domain.NewUniqueKey(smartblock.SmartBlockTypeSpaceView, spaceID)
	if err != nil {
		return err
	}
	changePayload, err := (&model.ObjectChangePayload{
		SmartBlockType: model.SmartBlockType(smartblock.SmartBlockTypeSpaceView),
		Key:            key.InternalKey(),
	}).Marshal()
	if err != nil {
		return err
	}
	payload := objecttree.ObjectTreeDerivePayload{
		ChangeType:    spacedomain.ChangeType,
		ChangePayload: changePayload,
		SpaceId:       techID,
		IsEncrypted:   true,
	}
	root, err := objecttree.DeriveObjectTreeRoot(payload, acl)
	if err != nil {
		return err
	}
	if _, err := st.TreeStorage(ctx, root.Id); err == nil {
		fmt.Printf("space view tree already exists: %s\n", root.Id)
		return nil
	}
	if _, err := st.CreateTreeStorage(ctx, treestorage.TreeStorageCreatePayload{
		RootRawChange: root,
		Changes:       []*treechangeproto.RawTreeChangeWithId{root},
		Heads:         []string{root.Id},
	}); err != nil {
		return err
	}
	fmt.Printf("space view root tree created: %s\n", root.Id)
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
