package parser

import (
	"reflect"
	"strings"
	"testing"
)

const runnerTagsPipeline = `
project "p" {
  ci_repo "ml" { path = "." }
  ci_task "train" {
    trigger     = "true"
    runner_tags = %s
    execute {
      repo   = "ml"
      script = "./train.sh"
    }
  }
}
`

func TestParseSource_RunnerTagsNormalised(t *testing.T) {
	proj, err := ParseSource([]byte(strings.Replace(runnerTagsPipeline, "%s", `["GPU", "linux", "gpu"]`, 1)), "p.tf")
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.Tasks[0].RunnerTags; !reflect.DeepEqual(got, []string{"gpu", "linux"}) {
		t.Fatalf("runner tags = %q, want [gpu linux]", got)
	}
}

func TestParseSource_InvalidRunnerTagNamesTask(t *testing.T) {
	_, err := ParseSource([]byte(strings.Replace(runnerTagsPipeline, "%s", `["has space"]`, 1)), "p.tf")
	if err == nil || !strings.Contains(err.Error(), `task "train"`) || !strings.Contains(err.Error(), "has space") {
		t.Fatalf("err = %v, want it to name the task and the tag", err)
	}
}
