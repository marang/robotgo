//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maximumGitHubResponse = 1024 * 1024
	gitHubAPIVersion      = "2022-11-28"
)

var registrationTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_=-]{20,512}$`)

// ProtectedRunIdentity binds one disposable runner to one approved workflow
// attempt and exact repository commit.
type ProtectedRunIdentity struct {
	Repository string
	Commit     string
	RunID      string
	RunAttempt int
	Cell       string
}

func (identity ProtectedRunIdentity) validate() error {
	if !repositoryPattern.MatchString(identity.Repository) {
		return errors.New("protected run repository is invalid")
	}
	if len(identity.Commit) != 40 {
		return errors.New("protected run commit is invalid")
	}
	if _, err := hex.DecodeString(identity.Commit); err != nil ||
		identity.Commit != strings.ToLower(identity.Commit) {
		return errors.New("protected run commit is invalid")
	}
	if _, err := parsePositiveInteger(identity.RunID); err != nil {
		return errors.New("protected run ID is invalid")
	}
	if identity.RunAttempt < 1 {
		return errors.New("protected run attempt is invalid")
	}
	if identity.Cell != "remote-desktop" && identity.Cell != "screencast" {
		return errors.New("protected run cell is invalid")
	}
	return nil
}

func (identity ProtectedRunIdentity) workflowName() string {
	if identity.Cell == "screencast" {
		return "ScreenCast E2E"
	}
	return "RemoteDesktop E2E"
}

func (identity ProtectedRunIdentity) runnerName(lane string) string {
	return "robotgo-" + lane + "-" + identity.RunID + "-" +
		strconv.Itoa(identity.RunAttempt) + "-" + identity.Cell
}

type protectedGitHub interface {
	ValidateRun(context.Context, ProtectedRunIdentity) error
	RunSucceeded(context.Context, ProtectedRunIdentity) (bool, error)
	RegistrationToken(context.Context, string) ([]byte, error)
	DeleteRunner(context.Context, string, string) error
}

type ghProtectedGitHub struct {
	commands CommandExecutor
}

type githubWorkflowRun struct {
	ID             int64            `json:"id"`
	Name           string           `json:"name"`
	HeadSHA        string           `json:"head_sha"`
	Event          string           `json:"event"`
	Status         string           `json:"status"`
	Conclusion     string           `json:"conclusion"`
	RunAttempt     int              `json:"run_attempt"`
	Repository     githubRepository `json:"repository"`
	HeadRepository githubRepository `json:"head_repository"`
}

type githubRepository struct {
	FullName string `json:"full_name"`
}

type githubRegistrationToken struct {
	Token     json.RawMessage `json:"token"`
	ExpiresAt time.Time       `json:"expires_at"`
}

type githubRunnerList struct {
	TotalCount int            `json:"total_count"`
	Runners    []githubRunner `json:"runners"`
}

type githubRunner struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
}

func newGHProtectedGitHub(commands CommandExecutor) protectedGitHub {
	if commands == nil {
		commands = systemCommandExecutor{}
	}
	return &ghProtectedGitHub{commands: commands}
}

func (client *ghProtectedGitHub) ValidateRun(
	ctx context.Context,
	identity ProtectedRunIdentity,
) error {
	if err := identity.validate(); err != nil {
		return err
	}
	run, err := client.workflowRun(ctx, identity)
	if err != nil {
		return err
	}
	switch run.Status {
	case "requested", "waiting", "pending", "queued", "in_progress":
		return nil
	default:
		return fmt.Errorf("protected runner workflow status %q is not runnable", run.Status)
	}
}

func (client *ghProtectedGitHub) RunSucceeded(
	ctx context.Context,
	identity ProtectedRunIdentity,
) (bool, error) {
	if err := identity.validate(); err != nil {
		return false, err
	}
	run, err := client.workflowRun(ctx, identity)
	if err != nil {
		return false, err
	}
	if run.Status != "completed" {
		return false, nil
	}
	if run.Conclusion != "success" {
		return false, fmt.Errorf(
			"protected runner workflow concluded with %q",
			run.Conclusion,
		)
	}
	return true, nil
}

func (client *ghProtectedGitHub) workflowRun(
	ctx context.Context,
	identity ProtectedRunIdentity,
) (githubWorkflowRun, error) {
	var run githubWorkflowRun
	if err := client.requestJSON(
		ctx,
		"GET",
		"repos/"+identity.Repository+"/actions/runs/"+identity.RunID,
		&run,
	); err != nil {
		return githubWorkflowRun{}, err
	}
	runID, _ := strconv.ParseInt(identity.RunID, 10, 64)
	if run.ID != runID ||
		run.Name != identity.workflowName() ||
		run.HeadSHA != identity.Commit ||
		run.RunAttempt != identity.RunAttempt ||
		run.Repository.FullName != identity.Repository ||
		run.HeadRepository.FullName != identity.Repository {
		return githubWorkflowRun{}, errors.New(
			"GitHub workflow run does not match protected runner approval",
		)
	}
	switch run.Event {
	case "workflow_dispatch", "push", "pull_request":
	default:
		return githubWorkflowRun{}, fmt.Errorf(
			"protected runner event %q is not supported",
			run.Event,
		)
	}
	return run, nil
}

func (client *ghProtectedGitHub) RegistrationToken(
	ctx context.Context,
	repository string,
) ([]byte, error) {
	if !repositoryPattern.MatchString(repository) {
		return nil, errors.New("protected runner repository is invalid")
	}
	var response githubRegistrationToken
	if err := client.requestJSON(
		ctx,
		"POST",
		"repos/"+repository+"/actions/runners/registration-token",
		&response,
	); err != nil {
		return nil, err
	}
	defer clearBytes(response.Token)
	if len(response.Token) < 2 ||
		response.Token[0] != '"' ||
		response.Token[len(response.Token)-1] != '"' ||
		!registrationTokenPattern.Match(response.Token[1:len(response.Token)-1]) {
		return nil, errors.New("GitHub returned an invalid runner registration token")
	}
	if !response.ExpiresAt.After(time.Now().Add(time.Minute)) {
		return nil, errors.New("GitHub runner registration token expires too soon")
	}
	token := bytes.Clone(response.Token[1 : len(response.Token)-1])
	return token, nil
}

func (client *ghProtectedGitHub) DeleteRunner(
	ctx context.Context,
	repository,
	name string,
) error {
	if !repositoryPattern.MatchString(repository) || name == "" {
		return errors.New("protected runner deletion identity is invalid")
	}
	var response githubRunnerList
	if err := client.requestJSON(
		ctx,
		"GET",
		"repos/"+repository+"/actions/runners?per_page=100",
		&response,
	); err != nil {
		return err
	}
	if response.TotalCount > len(response.Runners) {
		return errors.New("protected runner lookup exceeded one bounded API page")
	}
	var match *githubRunner
	for index := range response.Runners {
		if response.Runners[index].Name != name {
			continue
		}
		if match != nil {
			return errors.New("protected runner name is not unique")
		}
		match = &response.Runners[index]
	}
	if match == nil {
		return nil
	}
	if match.Busy || match.Status == "online" {
		return errors.New("protected runner is still active and cannot be deleted")
	}
	return client.requestNoContent(
		ctx,
		"DELETE",
		"repos/"+repository+"/actions/runners/"+strconv.FormatInt(match.ID, 10),
	)
}

func (client *ghProtectedGitHub) requestJSON(
	ctx context.Context,
	method,
	endpoint string,
	destination any,
) error {
	var output bytes.Buffer
	defer func() { clearBytes(output.Bytes()) }()
	writer := &boundedWriter{
		destination: &output,
		remaining:   maximumGitHubResponse,
	}
	if err := client.commands.Run(
		ctx,
		"gh",
		[]string{
			"api",
			"--method", method,
			"--header", "X-GitHub-Api-Version: " + gitHubAPIVersion,
			endpoint,
		},
		nil,
		writer,
	); err != nil {
		return fmt.Errorf("GitHub protected runner API request failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode GitHub protected runner response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("GitHub protected runner response has trailing data")
	}
	return nil
}

func (client *ghProtectedGitHub) requestNoContent(
	ctx context.Context,
	method,
	endpoint string,
) error {
	var output bytes.Buffer
	if err := client.commands.Run(
		ctx,
		"gh",
		[]string{
			"api",
			"--silent",
			"--method", method,
			"--header", "X-GitHub-Api-Version: " + gitHubAPIVersion,
			endpoint,
		},
		nil,
		&boundedWriter{
			destination: &output,
			remaining:   maximumGitHubResponse,
		},
	); err != nil {
		return fmt.Errorf("GitHub protected runner API mutation failed: %w", err)
	}
	if output.Len() != 0 {
		return errors.New("GitHub protected runner deletion returned unexpected data")
	}
	return nil
}

func parsePositiveInteger(value string) (int64, error) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, errors.New("integer is not canonical")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, errors.New("integer is not positive")
	}
	return parsed, nil
}
