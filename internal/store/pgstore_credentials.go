package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"kestrix/internal/credentials"
	"kestrix/internal/domain"
)

var ErrCredentialNotFound = errors.New("credential not found")
var ErrCredentialRevoked = errors.New("credential is revoked")

func newCredentialRef() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "cred_" + hex.EncodeToString(b), nil
}

func credentialAAD(meta domain.CredentialMetadata) []byte {
	b, _ := json.Marshal(struct{ Ref, Type, Provider, Host string }{meta.Ref, meta.Type, meta.Provider, meta.Host})
	return b
}

func secretBytes(c domain.Credential) []byte {
	b, _ := json.Marshal(c)
	return b
}

func (s *PGStore) CreateCredential(input domain.CredentialInput) (domain.CredentialMetadata, error) {
	ref, err := newCredentialRef()
	if err != nil {
		return domain.CredentialMetadata{}, fmt.Errorf("generate credential reference: %w", err)
	}
	normalized, err := credentials.Normalize(ref, input)
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	if s.masterKeys == nil {
		return domain.CredentialMetadata{}, credentials.ErrMasterKeyUnavailable
	}
	_, version, err := currentKeyVersion(s.masterKeys)
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	ciphertext, err := credentials.Encrypt(s.masterKeys, credentialAAD(normalized.Metadata), secretBytes(normalized.Secret))
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	var created, updated time.Time
	err = s.db.QueryRow(`INSERT INTO credentials (credential_ref,name,credential_type,provider,host,encrypted_payload,key_version) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at,updated_at`, ref, input.Name, normalized.Metadata.Type, input.Provider, input.Host, ciphertext, version).Scan(&created, &updated)
	if err != nil {
		return domain.CredentialMetadata{}, fmt.Errorf("store credential: %w", err)
	}
	normalized.Metadata.CreatedAt, normalized.Metadata.UpdatedAt = created, updated
	return normalized.Metadata, nil
}

func currentKeyVersion(provider credentials.MasterKeyProvider) ([]byte, int, error) {
	version, key, err := provider.Current()
	return key, version, err
}

func (s *PGStore) RotateCredential(ref string, input domain.CredentialInput) (domain.CredentialMetadata, error) {
	if s.masterKeys == nil {
		return domain.CredentialMetadata{}, credentials.ErrMasterKeyUnavailable
	}
	old, err := s.GetCredentialMetadata(ref)
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = old.Name
	}
	if input.Provider == "" {
		input.Provider = old.Provider
	}
	if input.Host == "" {
		input.Host = old.Host
	}
	normalized, err := credentials.Normalize(ref, input)
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	_, version, err := currentKeyVersion(s.masterKeys)
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	ciphertext, err := credentials.Encrypt(s.masterKeys, credentialAAD(normalized.Metadata), secretBytes(normalized.Secret))
	if err != nil {
		return domain.CredentialMetadata{}, err
	}
	err = s.db.QueryRow(`UPDATE credentials SET name=$2, credential_type=$3, provider=$4, host=$5, encrypted_payload=$6, key_version=$7, revoked=false, updated_at=now() WHERE credential_ref=$1 RETURNING created_at,updated_at,revoked`, ref, input.Name, normalized.Metadata.Type, input.Provider, input.Host, ciphertext, version).Scan(&normalized.Metadata.CreatedAt, &normalized.Metadata.UpdatedAt, &normalized.Metadata.Revoked)
	if err != nil {
		return domain.CredentialMetadata{}, fmt.Errorf("rotate credential: %w", err)
	}
	normalized.Metadata.Name, normalized.Metadata.Provider, normalized.Metadata.Host = input.Name, input.Provider, input.Host
	return normalized.Metadata, nil
}

func (s *PGStore) GetCredentialMetadata(ref string) (domain.CredentialMetadata, error) {
	var m domain.CredentialMetadata
	err := s.db.QueryRow(`SELECT credential_ref,name,credential_type,provider,host,created_at,updated_at,revoked FROM credentials WHERE credential_ref=$1`, ref).Scan(&m.Ref, &m.Name, &m.Type, &m.Provider, &m.Host, &m.CreatedAt, &m.UpdatedAt, &m.Revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CredentialMetadata{}, fmt.Errorf("%w: %s", ErrCredentialNotFound, ref)
	}
	if err != nil {
		return domain.CredentialMetadata{}, fmt.Errorf("read credential metadata: %w", err)
	}
	return m, nil
}

func (s *PGStore) ListCredentialMetadata() ([]domain.CredentialMetadata, error) {
	rows, err := s.db.Query(`SELECT credential_ref,name,credential_type,provider,host,created_at,updated_at,revoked FROM credentials ORDER BY name,credential_ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.CredentialMetadata
	for rows.Next() {
		var m domain.CredentialMetadata
		if err := rows.Scan(&m.Ref, &m.Name, &m.Type, &m.Provider, &m.Host, &m.CreatedAt, &m.UpdatedAt, &m.Revoked); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *PGStore) RevokeCredential(ref string) error {
	result, err := s.db.Exec(`UPDATE credentials SET revoked=true, updated_at=now() WHERE credential_ref=$1`, ref)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return fmt.Errorf("%w: %s", ErrCredentialNotFound, ref)
	}
	return err
}

func (s *PGStore) ResolveCredential(ctx context.Context, ref string) (domain.Credential, error) {
	if s.masterKeys == nil {
		return domain.Credential{}, credentials.ErrMasterKeyUnavailable
	}
	var m domain.CredentialMetadata
	var ciphertext []byte
	var keyVersion int
	err := s.db.QueryRowContext(ctx, `SELECT credential_ref,name,credential_type,provider,host,encrypted_payload,key_version,created_at,updated_at,revoked FROM credentials WHERE credential_ref=$1`, ref).Scan(&m.Ref, &m.Name, &m.Type, &m.Provider, &m.Host, &ciphertext, &keyVersion, &m.CreatedAt, &m.UpdatedAt, &m.Revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Credential{}, fmt.Errorf("%w: %s", ErrCredentialNotFound, ref)
	}
	if err != nil {
		return domain.Credential{}, err
	}
	if m.Revoked {
		return domain.Credential{}, fmt.Errorf("%w: %s", ErrCredentialRevoked, ref)
	}
	plain, err := credentials.Decrypt(s.masterKeys, credentialAAD(m), ciphertext)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("decrypt credential: %w", err)
	}
	var c domain.Credential
	if err := json.Unmarshal(plain, &c); err != nil {
		return domain.Credential{}, fmt.Errorf("decode credential: %w", err)
	}
	return c, nil
}
