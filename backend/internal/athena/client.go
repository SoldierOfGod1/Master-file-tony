// Package athena wraps AWS Athena for the Customer 360 usage panel.
// Surface area is intentionally tiny: a single Query method that
// takes SQL and returns string rows. The only caller today is the
// CDR usage lookup.
//
// Credentials follow the AWS default chain (ENV → shared config →
// IAM role → EC2/ECS metadata). If none resolve, New() returns
// ErrNotConfigured and the client should be treated as nil — NOT
// as a fatal startup error.
package athena

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// ErrNotConfigured signals the caller should skip Athena entirely.
var ErrNotConfigured = errors.New("athena: not configured")

// Config captures every knob the client needs. All fields are read
// from env (ATHENA_*) at startup; empty OutputS3 disables Athena.
type Config struct {
	Region       string
	Database     string
	Workgroup    string
	OutputS3     string
	QueryTimeout time.Duration
	PollInterval time.Duration
}

// Enabled is true when enough config is present to attempt a query.
func (c Config) Enabled() bool {
	return c.Region != "" && c.OutputS3 != ""
}

// Client is a thin wrapper over the Athena API.
type Client struct {
	api *athena.Client
	cfg Config
}

// New builds an Athena client. Returns ErrNotConfigured when config
// is missing fields or no AWS creds are available.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if !cfg.Enabled() {
		return nil, ErrNotConfigured
	}
	if cfg.QueryTimeout == 0 {
		cfg.QueryTimeout = 25 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 1 * time.Second
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	// Fail early when the credential chain is empty rather than
	// waiting for a cryptic 403 on first query.
	if _, err := aws.Credentials.Retrieve(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	return &Client{
		api: athena.NewFromConfig(aws),
		cfg: cfg,
	}, nil
}

// Query executes SQL and returns rows as string slices, including
// the header row first. Empty result → just the header. Parameter
// substitution is NOT supported: internal callers sanitise inputs.
func (c *Client) Query(ctx context.Context, sql string) ([][]string, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	qctx, cancel := context.WithTimeout(ctx, c.cfg.QueryTimeout)
	defer cancel()

	startInput := &athena.StartQueryExecutionInput{
		QueryString:         aws.String(sql),
		ResultConfiguration: &types.ResultConfiguration{OutputLocation: aws.String(c.cfg.OutputS3)},
	}
	if c.cfg.Database != "" {
		startInput.QueryExecutionContext = &types.QueryExecutionContext{
			Database: aws.String(c.cfg.Database),
		}
	}
	if c.cfg.Workgroup != "" {
		startInput.WorkGroup = aws.String(c.cfg.Workgroup)
	}
	start, err := c.api.StartQueryExecution(qctx, startInput)
	if err != nil {
		return nil, fmt.Errorf("athena start: %w", err)
	}
	qid := aws.ToString(start.QueryExecutionId)

	// On ctx cancel, best-effort stop so we don't leak a running
	// query that keeps billing until Athena server-side times it out.
	defer func() {
		if qctx.Err() != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = c.api.StopQueryExecution(stopCtx, &athena.StopQueryExecutionInput{
				QueryExecutionId: aws.String(qid),
			})
			stopCancel()
		}
	}()

	for {
		exec, err := c.api.GetQueryExecution(qctx, &athena.GetQueryExecutionInput{
			QueryExecutionId: aws.String(qid),
		})
		if err != nil {
			return nil, fmt.Errorf("athena get execution: %w", err)
		}
		state := exec.QueryExecution.Status.State
		switch state {
		case types.QueryExecutionStateSucceeded:
			return c.fetchResults(qctx, qid)
		case types.QueryExecutionStateFailed, types.QueryExecutionStateCancelled:
			reason := aws.ToString(exec.QueryExecution.Status.StateChangeReason)
			return nil, fmt.Errorf("athena query %s: %s", state, reason)
		}
		select {
		case <-qctx.Done():
			return nil, fmt.Errorf("athena poll: %w", qctx.Err())
		case <-time.After(c.cfg.PollInterval):
		}
	}
}

func (c *Client) fetchResults(ctx context.Context, qid string) ([][]string, error) {
	var out [][]string
	var token *string
	for {
		res, err := c.api.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(qid),
			NextToken:        token,
		})
		if err != nil {
			return nil, fmt.Errorf("athena results: %w", err)
		}
		if res.ResultSet == nil {
			break
		}
		for _, row := range res.ResultSet.Rows {
			cells := make([]string, 0, len(row.Data))
			for _, d := range row.Data {
				cells = append(cells, aws.ToString(d.VarCharValue))
			}
			out = append(out, cells)
		}
		if res.NextToken == nil {
			break
		}
		token = res.NextToken
	}
	return out, nil
}

// ConfigFromEnv reads ATHENA_* env vars into a Config. Kept for
// dev workflows where a developer exports env vars locally. UI-
// driven deployments should use ConfigFromSources which layers
// app_settings over env.
func ConfigFromEnv(getenv func(string) string) Config {
	trim := func(key string) string { return strings.TrimSpace(getenv(key)) }
	region := trim("ATHENA_REGION")
	if region == "" {
		region = "eu-west-1"
	}
	database := trim("ATHENA_DATABASE")
	if database == "" {
		database = "usage"
	}
	return Config{
		Region:       region,
		Database:     database,
		Workgroup:    trim("ATHENA_WORKGROUP"),
		OutputS3:     trim("ATHENA_OUTPUT"),
		QueryTimeout: 25 * time.Second,
		PollInterval: 1 * time.Second,
	}
}

// ConfigFromSources layers app_settings (authoritative) over env
// (fallback). UI edits in the Settings page take effect on the
// next restart without the user needing to touch env vars.
// `settings` is a pre-loaded map; callers read it from store.GetAllSettings().
func ConfigFromSources(settings map[string]string, getenv func(string) string) Config {
	cfg := ConfigFromEnv(getenv)
	if v := strings.TrimSpace(settings["athena.region"]); v != "" {
		cfg.Region = v
	}
	if v := strings.TrimSpace(settings["athena.database"]); v != "" {
		cfg.Database = v
	}
	if v := strings.TrimSpace(settings["athena.workgroup"]); v != "" {
		cfg.Workgroup = v
	}
	if v := strings.TrimSpace(settings["athena.output_s3"]); v != "" {
		cfg.OutputS3 = v
	}
	// Respect an explicit "disabled" flag even when other fields are set.
	if v := strings.ToLower(strings.TrimSpace(settings["athena.enabled"])); v == "false" || v == "0" || v == "no" {
		cfg.OutputS3 = "" // Enabled() gates on OutputS3, so clearing it disables.
	}
	return cfg
}
