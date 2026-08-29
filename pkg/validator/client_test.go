package validator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phantomguard/phantomguard/pkg/model"
)

func TestLookupMapsOnly200And404ToDefinitiveStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/pypi/real/json", "/real":
			writer.WriteHeader(http.StatusOK)
		case "/pypi/ghost/json", "/ghost":
			writer.WriteHeader(http.StatusNotFound)
		default:
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	client := testClient(server)
	if got := client.Lookup(context.Background(), model.PyPI, "real"); got != model.Exists {
		t.Fatalf("got %q", got)
	}
	if got := client.Lookup(context.Background(), model.NPM, "ghost"); got != model.Phantom {
		t.Fatalf("got %q", got)
	}
	if got := client.Lookup(context.Background(), model.NPM, "failing"); got != model.Unknown {
		t.Fatalf("got %q", got)
	}
}

func TestLookupTimeoutIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	client := testClient(server)
	client.Timeout = 25 * time.Millisecond
	if got := client.Lookup(context.Background(), model.NPM, "slow-package"); got != model.Unknown {
		t.Fatalf("timeout returned %q; want unknown", got)
	}
}

func TestValidNameRejectsUnsafeTokensBeforeHTTP(t *testing.T) {
	unsafe := []struct {
		ecosystem model.Ecosystem
		name      string
	}{
		{model.PyPI, "../../etc"},
		{model.NPM, "../../etc"},
		{model.NPM, "space name"},
		{model.PyPI, "\u03c0-package"},
		{model.NPM, "@scope/../../package"},
	}
	for _, test := range unsafe {
		if ValidName(test.ecosystem, test.name) {
			t.Errorf("%s token %q was accepted", test.ecosystem, test.name)
		}
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client := testClient(server)
	if got := client.Lookup(context.Background(), model.NPM, "../../etc"); got != model.Suspicious {
		t.Fatalf("got %q; want suspicious", got)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid token made %d HTTP requests", got)
	}
}

func TestLookupManyUsesBoundedConcurrentWorkers(t *testing.T) {
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := active.Add(1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		time.Sleep(20 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := testClient(server)
	client.Workers = 3
	candidates := make([]Candidate, 12)
	for index := range candidates {
		candidates[index] = Candidate{Ecosystem: model.NPM, Name: fmt.Sprintf("package-%d", index)}
	}
	results := client.LookupMany(context.Background(), candidates)
	if len(results) != len(candidates) {
		t.Fatalf("got %d results; want %d", len(results), len(candidates))
	}
	if got := peak.Load(); got != 3 {
		t.Fatalf("peak concurrent requests = %d; want 3", got)
	}
}

func TestLookupManyCompletesPartialFailuresWithoutMixingResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ok":
			writer.WriteHeader(http.StatusOK)
		case "/missing":
			writer.WriteHeader(http.StatusNotFound)
		case "/unavailable":
			writer.WriteHeader(http.StatusServiceUnavailable)
		case "/slow":
			<-request.Context().Done()
		}
	}))
	defer server.Close()
	client := testClient(server)
	client.Timeout = 25 * time.Millisecond
	candidates := []Candidate{
		{Ecosystem: model.NPM, Name: "ok"},
		{Ecosystem: model.NPM, Name: "missing"},
		{Ecosystem: model.NPM, Name: "unavailable"},
		{Ecosystem: model.NPM, Name: "slow"},
	}
	results := client.LookupMany(context.Background(), candidates)
	for candidate, want := range map[Candidate]model.Status{
		candidates[0]: model.Exists,
		candidates[1]: model.Phantom,
		candidates[2]: model.Unknown,
		candidates[3]: model.Unknown,
	} {
		if got := results[candidate]; got != want {
			t.Errorf("%s: got %q; want %q", candidate.Name, got, want)
		}
	}
}

func TestLookupManyHonorsScanBudgetAndReturnsEveryCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	client := testClient(server)
	client.Workers = 2
	client.Timeout = time.Second
	client.Budget = 25 * time.Millisecond
	candidates := make([]Candidate, 10)
	for index := range candidates {
		candidates[index] = Candidate{Ecosystem: model.NPM, Name: fmt.Sprintf("blocked-%d", index)}
	}
	started := time.Now()
	results := client.LookupMany(context.Background(), candidates)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("lookup batch ignored budget and took %s", elapsed)
	}
	if len(results) != len(candidates) {
		t.Fatalf("got %d results; want %d", len(results), len(candidates))
	}
	for _, candidate := range candidates {
		if got := results[candidate]; got != model.Unknown {
			t.Errorf("%s: got %q; want unknown", candidate.Name, got)
		}
	}
}

func TestLookupManyDeduplicatesCandidates(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := testClient(server)
	candidate := Candidate{Ecosystem: model.NPM, Name: "react"}
	results := client.LookupMany(context.Background(), []Candidate{candidate, candidate, candidate})
	if len(results) != 1 || results[candidate] != model.Exists {
		t.Fatalf("unexpected results: %#v", results)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("made %d HTTP requests for one unique candidate", got)
	}
}

func testClient(server *httptest.Server) *Client {
	client := NewClient()
	client.Endpoints = Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	client.Timeout = time.Second
	client.Budget = time.Second
	return client
}
