package secrets

import (
	"fmt"
	"os"

	"filippo.io/age"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/yaml"

	sops "github.com/getsops/sops/v3"
)

// Rotate adds and/or removes age recipients from an existing sops-encrypted
// file without decrypting secret values to disk. The data key is decrypted
// in memory, the recipient list is modified, and the data key is re-encrypted
// for the updated set of recipients.
func Rotate(filePath string, addKeys []string, removeKeys []string) error {
	// 1. Validate file exists
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// 2. Validate provided public keys
	addRecipients := make([]string, 0, len(addKeys))
	for _, k := range addKeys {
		_, err := age.ParseX25519Recipient(k)
		if err != nil {
			return fmt.Errorf("invalid age public key %q: %w", k, err)
		}
		addRecipients = append(addRecipients, k)
	}

	removeSet := make(map[string]bool, len(removeKeys))
	for _, k := range removeKeys {
		_, err := age.ParseX25519Recipient(k)
		if err != nil {
			return fmt.Errorf("invalid age public key %q: %w", k, err)
		}
		removeSet[k] = true
	}

	// 3. Load the encrypted sops tree
	store := &yaml.Store{}
	tree, err := store.LoadEncryptedFile(fileBytes)
	if err != nil {
		return fmt.Errorf("failed to load encrypted file: %w", err)
	}

	// 4. Get existing age recipients from key groups
	existingRecipients := make(map[string]bool)
	for _, kg := range tree.Metadata.KeyGroups {
		for _, key := range kg {
			if ageKey, ok := key.(*sopsage.MasterKey); ok {
				existingRecipients[ageKey.Recipient] = true
			}
		}
	}

	// 5. Validate removals
	for k := range removeSet {
		if !existingRecipients[k] {
			return fmt.Errorf("key %s is not a recipient of this file", k)
		}
	}

	// Count remaining after removals and before adds
	remainingCount := 0
	for r := range existingRecipients {
		if !removeSet[r] {
			remainingCount++
		}
	}
	// Count genuinely new adds (not already present)
	for _, k := range addRecipients {
		if !existingRecipients[k] || removeSet[k] {
			remainingCount++
		}
	}
	if remainingCount == 0 {
		return fmt.Errorf("cannot remove all recipients. At least one age key must remain")
	}

	// 6. Check if there's actually anything to do
	hasChanges := false
	for _, k := range addRecipients {
		if !existingRecipients[k] {
			hasChanges = true
			break
		}
	}
	if len(removeSet) > 0 {
		hasChanges = true
	}
	if !hasChanges {
		// All add keys are duplicates and no removals - no-op
		return nil
	}

	// 7. Decrypt the data key using our local age identity
	svcs := []keyservice.KeyServiceClient{keyservice.NewLocalClient()}
	dataKey, err := tree.Metadata.GetDataKeyWithKeyServices(svcs, nil)
	if err != nil {
		return fmt.Errorf("cannot rotate — unable to decrypt data key: %w", err)
	}

	// 8. Build updated key groups
	updatedKeyGroups := make([]sops.KeyGroup, len(tree.Metadata.KeyGroups))
	for i, kg := range tree.Metadata.KeyGroups {
		var newGroup sops.KeyGroup
		for _, key := range kg {
			if ageKey, ok := key.(*sopsage.MasterKey); ok {
				if removeSet[ageKey.Recipient] {
					continue // skip removed keys
				}
			}
			newGroup = append(newGroup, key)
		}
		updatedKeyGroups[i] = newGroup
	}

	// Add new keys to the first key group
	if len(updatedKeyGroups) == 0 {
		updatedKeyGroups = []sops.KeyGroup{{}}
	}
	for _, k := range addRecipients {
		if existingRecipients[k] && !removeSet[k] {
			continue // skip duplicates
		}
		mk, err := sopsage.MasterKeyFromRecipient(k)
		if err != nil {
			return fmt.Errorf("failed to create master key from recipient %s: %w", k, err)
		}
		updatedKeyGroups[0] = append(updatedKeyGroups[0], mk)
	}

	// 9. Update the tree's key groups
	tree.Metadata.KeyGroups = updatedKeyGroups

	// 10. Re-encrypt the data key for the new set of recipients
	errs := tree.Metadata.UpdateMasterKeysWithKeyServices(dataKey, svcs)
	for _, e := range errs {
		if e != nil {
			return fmt.Errorf("failed to update master keys: %w", e)
		}
	}

	// NOTE: We do NOT re-encrypt the tree data. The secret values remain
	// encrypted with the same data key. We only re-wrapped the data key
	// for the new set of recipients. The MAC stays valid since neither the
	// encrypted data nor the data key changed.

	// 11. Serialize the updated tree back to YAML
	output, err := store.EmitEncryptedFile(tree)
	if err != nil {
		return fmt.Errorf("failed to emit encrypted file: %w", err)
	}

	// 13. Atomic write: write to temp, then rename
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, output, 0600); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath) // clean up on failure
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

// ListRecipients returns the list of age recipient public keys from a
// sops-encrypted file's metadata.
func ListRecipients(filePath string) ([]string, error) {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	store := &yaml.Store{}
	tree, err := store.LoadEncryptedFile(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load encrypted file: %w", err)
	}

	var recipients []string
	for _, kg := range tree.Metadata.KeyGroups {
		for _, key := range kg {
			if ageKey, ok := key.(*sopsage.MasterKey); ok {
				recipients = append(recipients, ageKey.Recipient)
			}
		}
	}

	return recipients, nil
}
