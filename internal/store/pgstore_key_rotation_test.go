package store

import (
	"context"
	"errors"
	"testing"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
	"kestrix/internal/model"
)

func TestPG_RotateMasterKey_AllThreeColumnsReencrypted(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	// 1. Setup old and new key providers
	oldKey := make([]byte, 32)
	for i := range oldKey {
		oldKey[i] = byte(i + 1)
	}
	oldProv := credentials.StaticMasterKeyProvider{Version: 1, Keys: map[int][]byte{1: oldKey}}

	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 42)
	}
	newProv := credentials.StaticMasterKeyProvider{Version: 2, Keys: map[int][]byte{2: newKey}}

	pg.SetCredentialMasterKeyProvider(oldProv)

	// 2. Insert test credential
	credMeta, err := pg.CreateCredential(domain.CredentialInput{
		Name:     "deploy-key",
		Type:     "token",
		Provider: "gitlab",
		Host:     "gitlab.com",
		Token:    "glpat-supersecret",
	})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	// 3. Create project and run
	mustUpsert(t, pg, "rot-proj", "")
	snap := &model.Project{
		Name:  "rot-proj",
		Tasks: []domain.Task{{Name: "build"}},
	}
	runID := mustCreateRun(t, pg, "rot-proj", snap)

	// 4. Insert remote job with secrets
	jobID, err := pg.CreateRemoteJob(runID, "build", domain.RemoteJobPayload{
		Task: domain.Task{Name: "build"},
		Variables: map[string]string{
			"VAR1": "val1",
		},
		SecretValues: []string{"secret-payload-123"},
	})
	if err != nil {
		t.Fatalf("CreateRemoteJob: %v", err)
	}

	// 5. Insert run remote secrets snapshot
	err = pg.SetRemoteRunSnapshot(runID, []byte("tar-archive"), map[string]map[string]string{
		"build": {"RUN_VAR": "val2"},
	}, []string{"run-secret-456"})
	if err != nil {
		t.Fatalf("SetRemoteRunSnapshot: %v", err)
	}

	// Verify all three rows readable with old key before rotation
	cred, err := pg.ResolveCredential(ctx, credMeta.Ref)
	if err != nil || cred.Token != "glpat-supersecret" {
		t.Fatalf("ResolveCredential before: %v, %v", cred, err)
	}
	snapRes, vars, secVals, err := pg.GetRemoteRunSnapshot(runID)
	if err != nil || string(snapRes) != "tar-archive" || vars["build"]["RUN_VAR"] != "val2" || len(secVals) != 1 || secVals[0] != "run-secret-456" {
		t.Fatalf("GetRemoteRunSnapshot before: %v, vars=%v, secVals=%v", err, vars, secVals)
	}

	// 6. Execute rotation
	counts, err := pg.RotateMasterKey(ctx, oldProv, newProv)
	if err != nil {
		t.Fatalf("RotateMasterKey: %v", err)
	}
	if counts.Credentials != 1 {
		t.Errorf("counts.Credentials = %d, want 1", counts.Credentials)
	}
	if counts.RemoteJobSecrets != 1 {
		t.Errorf("counts.RemoteJobSecrets = %d, want 1", counts.RemoteJobSecrets)
	}
	if counts.RunSecrets != 1 {
		t.Errorf("counts.RunSecrets = %d, want 1", counts.RunSecrets)
	}

	// 7. Verify all three rows CANNOT be read with old key anymore
	pg.SetCredentialMasterKeyProvider(oldProv)
	if _, err := pg.ResolveCredential(ctx, credMeta.Ref); err == nil {
		t.Fatalf("ResolveCredential with old key succeeded after rotation, want decryption error")
	}
	if _, _, _, err := pg.GetRemoteRunSnapshot(runID); err == nil {
		t.Fatalf("GetRemoteRunSnapshot with old key succeeded after rotation, want decryption error")
	}

	// 8. Verify all three rows CAN be read with new key
	pg.SetCredentialMasterKeyProvider(newProv)
	cred, err = pg.ResolveCredential(ctx, credMeta.Ref)
	if err != nil {
		t.Fatalf("ResolveCredential with new key: %v", err)
	}
	if cred.Token != "glpat-supersecret" {
		t.Fatalf("cred.Token = %q, want glpat-supersecret", cred.Token)
	}

	snapRes, vars, secVals, err = pg.GetRemoteRunSnapshot(runID)
	if err != nil {
		t.Fatalf("GetRemoteRunSnapshot with new key: %v", err)
	}
	if string(snapRes) != "tar-archive" || vars["build"]["RUN_VAR"] != "val2" || len(secVals) != 1 || secVals[0] != "run-secret-456" {
		t.Fatalf("GetRemoteRunSnapshot content mismatch: %v, vars=%v, secVals=%v", err, vars, secVals)
	}

	// Check remote job row explicitly
	var jobCiphertext []byte
	err = pg.db.QueryRow(`SELECT secrets_enc FROM remote_jobs WHERE id = $1`, jobID).Scan(&jobCiphertext)
	if err != nil {
		t.Fatalf("query remote job secrets_enc: %v", err)
	}
	// Decrypting with old key fails
	if _, err := credentials.Decrypt(oldProv, remoteJobAAD(jobID), jobCiphertext); err == nil {
		t.Fatalf("decrypt remote job with old key succeeded after rotation, want decryption error")
	}
	// Decrypting with new key succeeds
	if plain, err := credentials.Decrypt(newProv, remoteJobAAD(jobID), jobCiphertext); err != nil {
		t.Fatalf("decrypt remote job with new key: %v", err)
	} else if len(plain) == 0 {
		t.Fatalf("decrypted remote job payload empty")
	}
}

func TestPG_RotateMasterKey_InjectedFailureRollsBack(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	oldKey := make([]byte, 32)
	for i := range oldKey {
		oldKey[i] = byte(i + 5)
	}
	oldProv := credentials.StaticMasterKeyProvider{Version: 1, Keys: map[int][]byte{1: oldKey}}

	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 88)
	}
	newProv := credentials.StaticMasterKeyProvider{Version: 2, Keys: map[int][]byte{2: newKey}}

	pg.SetCredentialMasterKeyProvider(oldProv)

	credMeta, err := pg.CreateCredential(domain.CredentialInput{
		Name:     "rollback-cred",
		Type:     "token",
		Provider: "gitlab",
		Host:     "gitlab.com",
		Token:    "glpat-rollback",
	})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	mustUpsert(t, pg, "rollback-proj", "")
	snap := &model.Project{
		Name:  "rollback-proj",
		Tasks: []domain.Task{{Name: "build"}},
	}
	runID := mustCreateRun(t, pg, "rollback-proj", snap)

	jobID, err := pg.CreateRemoteJob(runID, "build", domain.RemoteJobPayload{
		Task:         domain.Task{Name: "build"},
		SecretValues: []string{"job-secret"},
	})
	if err != nil {
		t.Fatalf("CreateRemoteJob: %v", err)
	}

	err = pg.SetRemoteRunSnapshot(runID, []byte("tar-archive"), map[string]map[string]string{
		"build": {"VAR": "val"},
	}, []string{"run-secret"})
	if err != nil {
		t.Fatalf("SetRemoteRunSnapshot: %v", err)
	}

	// Inject error during remote_jobs sweep (after credentials has already been rewritten in tx)
	injectedErr := errors.New("injected mid-sweep failure")
	keyRotationHook = func(table string, id any) error {
		if table == "remote_jobs" {
			return injectedErr
		}
		return nil
	}
	defer func() { keyRotationHook = nil }()

	// Execute rotation: should fail
	_, err = pg.RotateMasterKey(ctx, oldProv, newProv)
	if err == nil {
		t.Fatalf("RotateMasterKey: expected error, got nil")
	}
	if !errors.Is(err, injectedErr) {
		t.Fatalf("RotateMasterKey err = %v, want %v", err, injectedErr)
	}

	// Verify rollback: ALL three rows still decrypt with OLD key!
	pg.SetCredentialMasterKeyProvider(oldProv)
	cred, err := pg.ResolveCredential(ctx, credMeta.Ref)
	if err != nil || cred.Token != "glpat-rollback" {
		t.Fatalf("ResolveCredential after rollback: %v, cred=%v", err, cred)
	}

	snapRes, vars, secVals, err := pg.GetRemoteRunSnapshot(runID)
	if err != nil || string(snapRes) != "tar-archive" || vars["build"]["VAR"] != "val" || len(secVals) != 1 || secVals[0] != "run-secret" {
		t.Fatalf("GetRemoteRunSnapshot after rollback: %v, vars=%v, secVals=%v", err, vars, secVals)
	}

	var jobCiphertext []byte
	err = pg.db.QueryRow(`SELECT secrets_enc FROM remote_jobs WHERE id = $1`, jobID).Scan(&jobCiphertext)
	if err != nil {
		t.Fatalf("query remote job secrets_enc: %v", err)
	}
	if _, err := credentials.Decrypt(oldProv, remoteJobAAD(jobID), jobCiphertext); err != nil {
		t.Fatalf("decrypt remote job with old key failed after rollback: %v", err)
	}

	// Verify NONE decrypt with new key
	if _, err := credentials.Decrypt(newProv, remoteJobAAD(jobID), jobCiphertext); err == nil {
		t.Fatalf("decrypt remote job with new key succeeded after rollback, want error")
	}
	pg.SetCredentialMasterKeyProvider(newProv)
	if _, err := pg.ResolveCredential(ctx, credMeta.Ref); err == nil {
		t.Fatalf("ResolveCredential with new key succeeded after rollback, want error")
	}
}
