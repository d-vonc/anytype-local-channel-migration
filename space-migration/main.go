package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	anystore "github.com/anyproto/any-store"
	"github.com/anyproto/any-sync/commonspace/object/accountdata"
	"github.com/anyproto/any-sync/commonspace/object/acl/list"
	"github.com/anyproto/any-sync/commonspace/object/acl/recordverifier"
	"github.com/anyproto/any-sync/commonspace/spacestorage"
	"github.com/anyproto/any-sync/consensus/consensusproto"
	"github.com/anyproto/any-sync/util/cidutil"
	"github.com/anyproto/any-sync/util/crypto"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"golang.org/x/term"
)

const (
	metadataPath = "m/SLIP-0021/anytype/account/metadata"
)

var (
	oldVault           = flag.String("old-vault", "", "Path to old data/<account> vault directory")
	newVault           = flag.String("new-vault", "", "Path to new data/<account> vault directory")
	spaceID            = flag.String("space-id", "", "Space/channel ID to migrate")
	expectedOldAccount = flag.String("old-account", "", "Expected old account ID derived from the old mnemonic")
	expectedNewAccount = flag.String("new-account", "", "Expected new account ID derived from the new mnemonic")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := requireFlags(); err != nil {
		return err
	}
	ctx := context.Background()
	oldMnemonic, err := readSecret("Old mnemonic")
	if err != nil {
		return err
	}
	newMnemonic, err := readSecret("New mnemonic")
	if err != nil {
		return err
	}

	oldKeys, err := derive(oldMnemonic, *expectedOldAccount)
	if err != nil {
		return fmt.Errorf("old mnemonic: %w", err)
	}
	newKeys, err := derive(newMnemonic, *expectedNewAccount)
	if err != nil {
		return fmt.Errorf("new mnemonic: %w", err)
	}
	newMetadata, err := accountMetadataPayload(newKeys.Identity)
	if err != nil {
		return fmt.Errorf("new account metadata: %w", err)
	}

	if err := ensureAnytypeClosed(); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "anytype-space-migration-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	tmpStoreDir := filepath.Join(tmp, *spaceID)
	if err := os.MkdirAll(tmpStoreDir, 0700); err != nil {
		return err
	}
	oldStore := filepath.Join(*oldVault, "spaceStoreNew", *spaceID, "store.db")
	tmpStore := filepath.Join(tmpStoreDir, "store.db")
	if err := copyFile(oldStore, tmpStore, true); err != nil {
		return fmt.Errorf("copy old store to temp: %w", err)
	}

	head, err := addAclAccount(ctx, tmpStore, oldKeys.Identity, newKeys.Identity.GetPublic(), newMetadata)
	if err != nil {
		return fmt.Errorf("add ACL account: %w", err)
	}
	fmt.Printf("acl head after add: %s\n", head)

	stamp := time.Now().Format("20060102-150405")
	newSpaceDir := filepath.Join(*newVault, "spaceStoreNew", *spaceID)
	if err := backupIfExists(newSpaceDir, stamp); err != nil {
		return err
	}
	if err := os.MkdirAll(newSpaceDir, 0700); err != nil {
		return err
	}
	if err := copyFile(tmpStore, filepath.Join(newSpaceDir, "store.db"), true); err != nil {
		return fmt.Errorf("install new space store: %w", err)
	}

	oldObjectStore := filepath.Join(*oldVault, "objectstore", *spaceID)
	newObjectStore := filepath.Join(*newVault, "objectstore", *spaceID)
	if err := backupIfExists(newObjectStore, stamp); err != nil {
		return err
	}
	if err := copyDir(oldObjectStore, newObjectStore, true); err != nil {
		return fmt.Errorf("copy objectstore: %w", err)
	}

	if err := copyDir(filepath.Join(*oldVault, "flatfs"), filepath.Join(*newVault, "flatfs"), false); err != nil {
		return fmt.Errorf("merge flatfs: %w", err)
	}

	fmt.Println("storage copied with updated ACL")
	fmt.Println("note: this may still need a SpaceView registration in the new techspace before the UI lists it")
	return nil
}

func requireFlags() error {
	missing := []string{}
	for name, value := range map[string]string{
		"old-vault":   *oldVault,
		"new-vault":   *newVault,
		"space-id":    *spaceID,
		"old-account": *expectedOldAccount,
		"new-account": *expectedNewAccount,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, "-"+name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
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

func derive(mnemonic, expected string) (crypto.DerivationResult, error) {
	keys, err := crypto.Mnemonic(mnemonic).DeriveKeys(0)
	if err != nil {
		return crypto.DerivationResult{}, err
	}
	account := keys.Identity.GetPublic().Account()
	if account != expected {
		return crypto.DerivationResult{}, fmt.Errorf("derived %s, expected %s", account, expected)
	}
	return keys, nil
}

func accountMetadataPayload(accountKey crypto.PrivKey) ([]byte, error) {
	raw, err := accountKey.Raw()
	if err != nil {
		return nil, err
	}
	symKey, err := crypto.DeriveSymmetricKey(raw, metadataPath)
	if err != nil {
		return nil, err
	}
	symKeyProto, err := symKey.Marshall()
	if err != nil {
		return nil, err
	}
	meta := &model.Metadata{
		Payload: &model.MetadataPayloadOfIdentity{
			Identity: &model.MetadataPayloadIdentityPayload{
				ProfileSymKey: symKeyProto,
			},
		},
	}
	return meta.Marshal()
}

func addAclAccount(ctx context.Context, storePath string, oldSignKey crypto.PrivKey, newPubKey crypto.PubKey, metadata []byte) (string, error) {
	db, err := anystore.Open(ctx, storePath, nil)
	if err != nil {
		return "", err
	}
	defer db.Close()

	st, err := spacestorage.New(ctx, *spaceID, db)
	if err != nil {
		return "", err
	}
	aclStorage, err := st.AclStorage()
	if err != nil {
		return "", err
	}
	peerKey, _, err := crypto.GenerateRandomEd25519KeyPair()
	if err != nil {
		return "", err
	}
	acc := accountdata.New(peerKey, oldSignKey)
	acl, err := list.BuildAclListWithIdentity(acc, aclStorage, recordverifier.NewValidateFull())
	if err != nil {
		return "", err
	}
	defer acl.Close(ctx)

	if !acl.AclState().Permissions(newPubKey).NoPermissions() {
		return acl.Head().Id, nil
	}
	raw, err := acl.RecordBuilder().BuildAccountsAdd(list.AccountsAddPayload{
		Additions: []list.AccountAdd{{
			Identity:    newPubKey,
			Permissions: list.AclPermissionsWriter,
			Metadata:    metadata,
		}},
	})
	if err != nil {
		return "", err
	}
	rawWithID, err := withCID(raw)
	if err != nil {
		return "", err
	}
	if err := acl.AddRawRecord(rawWithID); err != nil {
		return "", err
	}
	return rawWithID.Id, nil
}

func withCID(raw *consensusproto.RawRecord) (*consensusproto.RawRecordWithId, error) {
	payload, err := raw.MarshalVT()
	if err != nil {
		return nil, err
	}
	id, err := cidutil.NewCidFromBytes(payload)
	if err != nil {
		return nil, err
	}
	return &consensusproto.RawRecordWithId{Payload: payload, Id: id}, nil
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

func backupIfExists(path, stamp string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	backup := path + ".backup-" + stamp
	if _, err := os.Stat(backup); err == nil {
		return fmt.Errorf("backup path already exists: %s", backup)
	}
	return os.Rename(path, backup)
}

func copyDir(src, dst string, overwrite bool) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target, overwrite)
	})
}

func copyFile(src, dst string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
