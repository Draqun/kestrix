# Spec Delta: http-server-hardening

## ADDED Requirements

### Requirement: Configurable CORS support
The HTTP server SHALL support an opt-in `KESTRIX_HTTP_CORS_ALLOWED_ORIGINS` setting that emits appropriate CORS headers and handles preflight `OPTIONS` requests for matching external origins.

#### Scenario: Allowed origin receives CORS headers
- **WHEN** `KESTRIX_HTTP_CORS_ALLOWED_ORIGINS` is configured with `https://portal.example.com`
- **AND** a request arrives with header `Origin: https://portal.example.com`
- **THEN** the response includes `Access-Control-Allow-Origin: https://portal.example.com`
- **AND** preflight `OPTIONS` requests respond with HTTP 200 and allowed methods

#### Scenario: Disallowed origin receives no CORS headers
- **WHEN** a request arrives with an origin not in the allowed list
- **THEN** no `Access-Control-Allow-Origin` header is returned
