package squeeze

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
)

// InspectProjectRLS explicitly delegates live inspection to the generated
// application command. That command loads the application's own configuration
// and compares its generated desired state with PostgreSQL catalogs.
func InspectProjectRLS(projectDir string) ([]LiveRLSObservation, error) {
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "run", "./cmd/server/", "rls:status")
	cmd.Dir = project.Dir
	cmd.Env = os.Environ()
	for key, value := range generator.ParseDotEnv(filepath.Join(project.Dir, ".env")) {
		if os.Getenv(key) == "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("live RLS inspection failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	observations, err := ParseLiveRLSStatus(stdout.String())
	if err != nil {
		return nil, err
	}
	return observations, nil
}

// ParseLiveRLSStatus converts the stable generated rls:status output into the
// sanitized evidence consumed by Squeeze's live-only rules.
func ParseLiveRLSStatus(output string) ([]LiveRLSObservation, error) {
	var result []LiveRLSObservation
	var current *LiveRLSObservation
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") {
			if current == nil {
				return nil, fmt.Errorf("invalid rls:status output: problem without table")
			}
			problem := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if current.Detail != "" {
				current.Detail += "; "
			}
			current.Detail += problem
			lower := strings.ToLower(problem)
			switch {
			case strings.Contains(lower, "rls disabled"):
				current.Enabled = false
			case strings.Contains(lower, "rls not forced"):
				current.Forced = false
			case strings.Contains(lower, "superuser"):
				current.RuntimeSuperuser = true
			case strings.Contains(lower, "bypassrls"):
				current.RuntimeBypass = true
			case strings.Contains(lower, "owns table"):
				current.RuntimeOwner = true
			case strings.Contains(lower, "unexpected permissive policy"):
				current.ManualPermissive, current.Drift = true, true
			default:
				current.Drift = true
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(line, "fingerprint=") {
			continue // generated command initialization may print informational lines
		}
		result = append(result, LiveRLSObservation{Table: fields[0], Enabled: true, Forced: true})
		current = &result[len(result)-1]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
