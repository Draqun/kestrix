## ADDED Requirements

### Requirement: Signing key rotation

Replacing the configured token signing key and restarting Kestrix SHALL be
the supported way to rotate it. After the restart every token signed with
the previous key SHALL be rejected as invalid, so every session ends, while
runner tokens and the metrics token SHALL keep working. The deployment
documentation SHALL describe this procedure and SHALL list it in a checklist
for a suspected key leak, together with master key rotation.

#### Scenario: Old sessions end after rotation
- **WHEN** an operator replaces the signing key file and restarts Kestrix
- **THEN** a token issued before the restart SHALL be rejected with `401`
- **AND** signing in again SHALL issue a token that is accepted

#### Scenario: Runners unaffected
- **WHEN** the signing key is rotated
- **THEN** a registered runner SHALL keep authenticating with its existing token
