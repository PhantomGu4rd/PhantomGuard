package validator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/phantomguard/phantomguard/pkg/model"
)

var (
	pypiName = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9._-]*$`)
	npmName  = regexp.MustCompile(`(?i)^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
)

// Endpoints allows offline tests to replace public registries with httptest servers.
type Endpoints struct {
	PyPI string
	NPM  string
}

// Client uses the standard library HTTP client with bounded requests and a scan-wide budget.
type Client struct {
	HTTPClient *http.Client
	Endpoints  Endpoints
	Timeout    time.Duration
	Budget     time.Duration
	Workers    int
}

// NewClient builds the production registry client.
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 3 * time.Second},
		Endpoints:  newClientEndpoints(),
		Timeout:    3 * time.Second,
		Budget:     8 * time.Second,
		Workers:    8,
	}
}

// ValidName checks a candidate before it is allowed into an HTTP path.
func ValidName(ecosystem model.Ecosystem, name string) bool {
	switch ecosystem {
	case model.PyPI:
		return pypiName.MatchString(name)
	case model.NPM:
		return npmName.MatchString(name)
	default:
		return false
	}
}

// Lookup performs one public GET. Transport or non-200/404 failures are Unknown, never Exists.
func (c *Client) Lookup(ctx context.Context, ecosystem model.Ecosystem, name string) model.Status {
	if !ValidName(ecosystem, name) {
		return model.Suspicious
	}
	base := c.Endpoints.PyPI
	path := url.PathEscape(name) + "/json"
	if ecosystem == model.NPM {
		base = c.Endpoints.NPM
		path = url.PathEscape(name)
	}
	requestContext, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, strings.TrimSuffix(base, "/")+"/"+path, nil)
	if err != nil {
		return model.Unknown
	}
	if ecosystem == model.NPM {
		request.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return model.Unknown
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		return model.Exists
	case http.StatusNotFound:
		return model.Phantom
	default:
		return model.Unknown
	}
}

// Candidate is a unique registry lookup requested by the scanner.
type Candidate struct {
	Ecosystem model.Ecosystem
	Name      string
}

// LookupMany validates independent packages concurrently with goroutines, channels, and a scan budget.
func (c *Client) LookupMany(ctx context.Context, candidates []Candidate) map[Candidate]model.Status {
	results := make(map[Candidate]model.Status, len(candidates))
	if len(candidates) == 0 {
		return results
	}
	// A scanner normally de-duplicates candidates before this point, but the
	// client is also used directly by callers such as tests and future commands.
	// Keep this boundary idempotent so one batch never repeats an HTTP lookup.
	unique := make([]Candidate, 0, len(candidates))
	seen := make(map[Candidate]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	budgetContext, cancel := context.WithTimeout(ctx, c.Budget)
	defer cancel()
	workers := c.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(unique) {
		workers = len(unique)
	}
	jobs := make(chan Candidate)
	responses := make(chan struct {
		candidate Candidate
		status    model.Status
	}, len(unique))
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for candidate := range jobs {
				status := c.Lookup(budgetContext, candidate.Ecosystem, candidate.Name)
				responses <- struct {
					candidate Candidate
					status    model.Status
				}{candidate, status}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, candidate := range unique {
			select {
			case jobs <- candidate:
			case <-budgetContext.Done():
				return
			}
		}
	}()
	go func() { wait.Wait(); close(responses) }()
	for response := range responses {
		results[response.candidate] = response.status
	}
	for _, candidate := range unique {
		if _, ok := results[candidate]; !ok {
			results[candidate] = model.Unknown
		}
	}
	return results
}

// VerifyExists supports remediation and AI validation without exposing HTTP implementation details.
func (c *Client) VerifyExists(ctx context.Context, ecosystem model.Ecosystem, name string) error {
	if status := c.Lookup(ctx, ecosystem, name); status != model.Exists {
		return fmt.Errorf("%s is %s in %s", name, status, ecosystem)
	}
	return nil
}
