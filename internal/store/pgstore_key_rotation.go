package store

import (
	"context"
	"fmt"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

// KeyRotationCounts reports the number of ciphertexts re-encrypted across
// credentials, remote jobs, and runs during a master-key rotation.
type KeyRotationCounts struct {
	Credentials      int
	RemoteJobSecrets int
	RunSecrets       int
}

// keyRotationHook allows tests to inject a failure at a specific row during the sweep.
var keyRotationHook func(table string, id any) error

// RotateMasterKey re-encrypts every stored ciphertext in a single transaction
// using credentials.Rewrap and validates each rewritten row with newProvider
// before commit. If an error occurs on any row, the transaction rolls back,
// leaving every row under oldProvider.
func (s *PGStore) RotateMasterKey(ctx context.Context, oldProvider, newProvider credentials.MasterKeyProvider) (KeyRotationCounts, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return KeyRotationCounts{}, fmt.Errorf("begin key rotation transaction: %w", err)
	}
	defer tx.Rollback()

	var counts KeyRotationCounts

	// 1. Credentials sweep
	credRows, err := tx.QueryContext(ctx, `SELECT credential_ref, name, credential_type, provider, host, encrypted_payload FROM credentials FOR UPDATE`)
	if err != nil {
		return KeyRotationCounts{}, fmt.Errorf("query credentials for update: %w", err)
	}
	type credItem struct {
		meta domain.CredentialMetadata
		enc  []byte
	}
	var credentialsToUpdate []credItem
	for credRows.Next() {
		var (
			ref, name, credType, prov, host string
			payload                         []byte
		)
		if err := credRows.Scan(&ref, &name, &credType, &prov, &host, &payload); err != nil {
			_ = credRows.Close()
			return KeyRotationCounts{}, fmt.Errorf("scan credential row: %w", err)
		}
		credentialsToUpdate = append(credentialsToUpdate, credItem{
			meta: domain.CredentialMetadata{
				Ref:      ref,
				Name:     name,
				Type:     credType,
				Provider: prov,
				Host:     host,
			},
			enc: payload,
		})
	}
	if err := credRows.Close(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("close credential rows: %w", err)
	}
	if err := credRows.Err(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("credential rows iteration: %w", err)
	}

	newVersion, _, err := newProvider.Current()
	if err != nil {
		return KeyRotationCounts{}, fmt.Errorf("get new key version: %w", err)
	}

	for _, item := range credentialsToUpdate {
		if keyRotationHook != nil {
			if err := keyRotationHook("credentials", item.meta.Ref); err != nil {
				return KeyRotationCounts{}, err
			}
		}
		aad := credentialAAD(item.meta)
		rewritten, err := credentials.Rewrap(oldProvider, newProvider, aad, item.enc)
		if err != nil {
			return KeyRotationCounts{}, fmt.Errorf("rewrap credential %s: %w", item.meta.Ref, err)
		}
		if _, err := credentials.Decrypt(newProvider, aad, rewritten); err != nil {
			return KeyRotationCounts{}, fmt.Errorf("verify credential %s: %w", item.meta.Ref, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE credentials SET encrypted_payload = $1, key_version = $2, updated_at = now() WHERE credential_ref = $3`, rewritten, newVersion, item.meta.Ref); err != nil {
			return KeyRotationCounts{}, fmt.Errorf("update credential %s: %w", item.meta.Ref, err)
		}
		counts.Credentials++
	}

	// 2. Remote jobs sweep
	jobRows, err := tx.QueryContext(ctx, `SELECT id, secrets_enc FROM remote_jobs WHERE secrets_enc IS NOT NULL AND length(secrets_enc) > 0 FOR UPDATE`)
	if err != nil {
		return KeyRotationCounts{}, fmt.Errorf("query remote jobs for update: %w", err)
	}
	type jobItem struct {
		id  int
		enc []byte
	}
	var jobsToUpdate []jobItem
	for jobRows.Next() {
		var (
			id  int
			enc []byte
		)
		if err := jobRows.Scan(&id, &enc); err != nil {
			_ = jobRows.Close()
			return KeyRotationCounts{}, fmt.Errorf("scan remote job row: %w", err)
		}
		jobsToUpdate = append(jobsToUpdate, jobItem{id: id, enc: enc})
	}
	if err := jobRows.Close(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("close remote job rows: %w", err)
	}
	if err := jobRows.Err(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("remote job rows iteration: %w", err)
	}

	for _, item := range jobsToUpdate {
		if keyRotationHook != nil {
			if err := keyRotationHook("remote_jobs", item.id); err != nil {
				return KeyRotationCounts{}, err
			}
		}
		aad := remoteJobAAD(item.id)
		rewritten, err := credentials.Rewrap(oldProvider, newProvider, aad, item.enc)
		if err != nil {
			return KeyRotationCounts{}, fmt.Errorf("rewrap remote_job %d: %w", item.id, err)
		}
		if _, err := credentials.Decrypt(newProvider, aad, rewritten); err != nil {
			return KeyRotationCounts{}, fmt.Errorf("verify remote_job %d: %w", item.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE remote_jobs SET secrets_enc = $1, updated_at = now() WHERE id = $2`, rewritten, item.id); err != nil {
			return KeyRotationCounts{}, fmt.Errorf("update remote_job %d: %w", item.id, err)
		}
		counts.RemoteJobSecrets++
	}

	// 3. Runs sweep
	runRows, err := tx.QueryContext(ctx, `SELECT id, remote_secrets_enc FROM runs WHERE remote_secrets_enc IS NOT NULL AND length(remote_secrets_enc) > 0 FOR UPDATE`)
	if err != nil {
		return KeyRotationCounts{}, fmt.Errorf("query runs for update: %w", err)
	}
	type runItem struct {
		id  int
		enc []byte
	}
	var runsToUpdate []runItem
	for runRows.Next() {
		var (
			id  int
			enc []byte
		)
		if err := runRows.Scan(&id, &enc); err != nil {
			_ = runRows.Close()
			return KeyRotationCounts{}, fmt.Errorf("scan run row: %w", err)
		}
		runsToUpdate = append(runsToUpdate, runItem{id: id, enc: enc})
	}
	if err := runRows.Close(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("close run rows: %w", err)
	}
	if err := runRows.Err(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("run rows iteration: %w", err)
	}

	for _, item := range runsToUpdate {
		if keyRotationHook != nil {
			if err := keyRotationHook("runs", item.id); err != nil {
				return KeyRotationCounts{}, err
			}
		}
		aad := remoteRunAAD(item.id)
		rewritten, err := credentials.Rewrap(oldProvider, newProvider, aad, item.enc)
		if err != nil {
			return KeyRotationCounts{}, fmt.Errorf("rewrap run %d: %w", item.id, err)
		}
		if _, err := credentials.Decrypt(newProvider, aad, rewritten); err != nil {
			return KeyRotationCounts{}, fmt.Errorf("verify run %d: %w", item.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET remote_secrets_enc = $1 WHERE id = $2`, rewritten, item.id); err != nil {
			return KeyRotationCounts{}, fmt.Errorf("update run %d: %w", item.id, err)
		}
		counts.RunSecrets++
	}

	if err := tx.Commit(); err != nil {
		return KeyRotationCounts{}, fmt.Errorf("commit key rotation: %w", err)
	}
	return counts, nil
}
