package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// HookWorkflow is what Release Planner reads from a hook workflow's file.
type HookWorkflow struct {
	// Name is the name: the workflow declares, or "" when it declares none, or one GitHub
	// would evaluate as an expression where the Release workflow passes it to the report.
	Name string
	// Environment is the GitHub environment every job of the workflow names, which holds its
	// credentials.
	Environment string
}

// ReadHook reads a hook workflow's file in .github/workflows under root, such as a pre-publish
// workflow's, and
// checks the parts of it Release Planner relies on: every job names the same GitHub environment
// literally, which isn't one of Release Planner's own; no job calls another workflow, which
// couldn't name one; and it requires no secrets, since the Release workflow passes none.
// problem says what to fix, or is "" when the workflow fits. A missing file is no problem
// here; callers report it.
func ReadHook(root, workflow string) (HookWorkflow, string, error) {
	var w HookWorkflow
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", workflow))
	if errors.Is(err, fs.ErrNotExist) {
		return w, "", nil
	}
	if err != nil {
		return w, "", err
	}
	var wf struct {
		Name string `yaml:"name"`
		On   struct {
			Call struct {
				Secrets map[string]struct {
					Required bool `yaml:"required"`
				} `yaml:"secrets"`
			} `yaml:"workflow_call"`
		} `yaml:"on"`
		Jobs yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return w, "", fmt.Errorf(".github/workflows/%s: %v", workflow, err)
	}
	if n := strings.TrimSpace(wf.Name); !strings.Contains(n, "${{") && !strings.ContainsFunc(n, unicode.IsControl) && len(n) <= 100 {
		w.Name = n
	}

	if wf.Jobs.Kind != yaml.MappingNode || len(wf.Jobs.Content) == 0 {
		return w, "has no jobs", nil
	}
	var environments []string
	// Jobs in the order the file lists them, so the message names the first one to fix.
	for i := 0; i+1 < len(wf.Jobs.Content); i += 2 {
		id := wf.Jobs.Content[i].Value
		var job struct {
			Uses        string `yaml:"uses"`
			Environment any    `yaml:"environment"`
		}
		if err := wf.Jobs.Content[i+1].Decode(&job); err != nil {
			return w, "", fmt.Errorf(".github/workflows/%s: job %s: %v", workflow, id, err)
		}
		if job.Uses != "" {
			return w, fmt.Sprintf("job %s calls another workflow; every job must run in a GitHub environment itself", id), nil
		}
		env := job.Environment
		if m, ok := env.(map[string]any); ok {
			env = m["name"]
		}
		name, _ := env.(string)
		switch {
		case name == "":
			return w, fmt.Sprintf("job %s runs in no environment; every job must name the same GitHub environment, such as environment: production", id), nil
		case strings.Contains(name, "${{"):
			return w, fmt.Sprintf("job %s names its environment with an expression; name it literally, such as environment: production", id), nil
		case !slices.Contains(environments, name):
			environments = append(environments, name)
		}
	}
	if len(environments) > 1 {
		slices.Sort(environments)
		return w, fmt.Sprintf("jobs run in different environments, %s and %s; every job must name the same one",
			strings.Join(environments[:len(environments)-1], ", "), environments[len(environments)-1]), nil
	}
	switch e := environments[0]; {
	case !environment.MatchString(e):
		return w, fmt.Sprintf("%q isn't an environment name; use letters, digits, and . _ -", e), nil
	case strings.EqualFold(e, ReleaseEnvironment) || strings.EqualFold(e, DispatchEnvironment):
		return w, fmt.Sprintf("%s is an environment Release Planner uses for its own credentials; use one of its own, such as production", e), nil
	}
	w.Environment = environments[0]

	var required []string
	for secret, spec := range wf.On.Call.Secrets {
		if spec.Required {
			required = append(required, secret)
		}
	}
	if len(required) > 0 {
		slices.Sort(required)
		return w, fmt.Sprintf("requires secrets the Release workflow doesn't pass (%s); store them in the %s environment instead", strings.Join(required, ", "), w.Environment), nil
	}
	return w, "", nil
}
