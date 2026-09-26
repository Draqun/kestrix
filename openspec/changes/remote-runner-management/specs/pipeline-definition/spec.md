## ADDED Requirements

### Requirement: Task runner tags

A `ci_task` block SHALL accept an optional `runner_tags` list of strings
naming the tags a runner must carry to execute the task in `remote` mode.
Tags SHALL be normalised (trimmed, lowercased, deduplicated) and SHALL match
`[a-z0-9._-]{1,64}`, at most 32 per task. Outside `remote` mode the attribute
SHALL be accepted and ignored.

#### Scenario: Declare runner tags
- **WHEN** a user writes `ci_task "train" { trigger = "true" runner_tags = ["GPU"] execute { repo = "ml" script = "./train.sh" } }`
- **THEN** the task SHALL be registered with runner tags `["gpu"]`

#### Scenario: Invalid runner tag rejected
- **WHEN** a `ci_task` declares a runner tag containing a space
- **THEN** parsing SHALL fail with an error naming the task and the tag

#### Scenario: Ignored outside remote mode
- **WHEN** a project in `docker` mode runs a task declaring `runner_tags`
- **THEN** the task SHALL execute as it would without the attribute
