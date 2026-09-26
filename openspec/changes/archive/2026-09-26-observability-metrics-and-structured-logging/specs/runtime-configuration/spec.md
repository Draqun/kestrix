## ADDED Requirements

### Requirement: Observability settings follow the precedence rule

The settings `metrics.enabled` (`KESTRIX_METRICS_ENABLED`), `metrics.token`
(`KESTRIX_METRICS_TOKEN`), and `logging.level` (`KESTRIX_LOG_LEVEL`) SHALL be
resolved by the same precedence as every other multi-form setting (explicit
flag, then config file, then environment, then default), SHALL appear in the
startup source report, and SHALL fail startup with an error naming the setting
when the resolved value is not one of its accepted values. `metrics.token` is a
secret: its value SHALL NOT appear in the source report, in any log line, or in
any API response.

#### Scenario: File wins over environment
- **WHEN** `config.yaml` sets `logging.level: debug` and the environment sets `KESTRIX_LOG_LEVEL=warn`
- **THEN** the effective level is `debug` and the source report attributes it to the file

#### Scenario: Environment fills an absent key
- **WHEN** the file omits `metrics.enabled` and the environment sets `KESTRIX_METRICS_ENABLED=true`
- **THEN** metrics are enabled and the source report attributes the setting to the environment

#### Scenario: Invalid value
- **WHEN** `logging.level` resolves to a value outside `debug`, `info`, `warn`, `error`, or `metrics.enabled` to a non-boolean
- **THEN** startup fails naming that setting and never reveals other values

#### Scenario: Scrape token stays private
- **WHEN** `metrics.token` is set by any form
- **THEN** the startup source report SHALL name the setting and its source without its value
- **AND** no log line SHALL contain the token
