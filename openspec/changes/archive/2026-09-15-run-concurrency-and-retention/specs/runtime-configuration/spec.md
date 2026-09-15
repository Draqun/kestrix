## ADDED Requirements

### Requirement: Run scheduling settings follow the precedence rule

The settings `runner.max_concurrent_runs` (`KESTRIX_RUNNER_MAX_CONCURRENT_RUNS`), `runner.run_history_limit` (`KESTRIX_RUN_HISTORY_LIMIT`), and `runner.max_output_bytes` (`KESTRIX_RUNNER_MAX_OUTPUT_BYTES`) SHALL be resolved by the same precedence as every other multi-form setting (explicit flag, then config file, then environment, then default), SHALL appear in the startup source report, and SHALL each fail startup with an error naming the setting when the resolved value is not a positive integer.

#### Scenario: File wins over environment
- **WHEN** `config.yaml` sets `runner.max_concurrent_runs: 4` and the environment sets `KESTRIX_RUNNER_MAX_CONCURRENT_RUNS=1`
- **THEN** the effective limit is `4` and the source report attributes it to the file

#### Scenario: Environment fills an absent key
- **WHEN** the file omits `runner.run_history_limit` and the environment sets `KESTRIX_RUN_HISTORY_LIMIT=500`
- **THEN** the effective limit is `500` and the source report attributes it to the environment

#### Scenario: Invalid value
- **WHEN** any of the three resolves to `0`, a negative number, or a non-integer
- **THEN** startup fails naming that setting and never reveals other values
