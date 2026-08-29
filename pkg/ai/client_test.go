package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	pgmath "github.com/phantomguard/phantomguard/pkg/math"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

func TestExplicitClientConstructionRequiresLocalCredential(t *testing.T) {
	if _, err := NewClientWithKey(OpenAI, "", ""); err == nil {
		t.Fatal("AI client accepted an empty local credential")
	}
	client, err := NewClientWithKey(Provider("grok"), "", "local-key")
	if err != nil {
		t.Fatal(err)
	}
	if client.Provider != XAI || client.APIKey != "local-key" {
		t.Fatalf("explicit client construction lost provider or key: %#v", client)
	}
}

func TestSuggestionRequiresStrictJSONAndRegistryVerification(t *testing.T) {
	var registryHits atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		registryHits.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()
	aiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"output_text":"{\"package\":\"react\"}"}`))
	}))
	defer aiServer.Close()
	registry := validator.NewClient()
	registry.Endpoints = validator.Endpoints{PyPI: registryServer.URL, NPM: registryServer.URL}
	client := &Client{HTTPClient: aiServer.Client(), Endpoint: aiServer.URL, APIKey: "test", Model: "test"}
	suggestion, err := client.Suggest(context.Background(), model.Finding{Name: "reaxt", Ecosystem: model.NPM, Path: "app.js", Line: 1, Snippet: "require('reaxt')"}, registry, pgmath.NewRiskEngine())
	if err != nil || suggestion != "react" {
		t.Fatalf("suggestion=%q err=%v", suggestion, err)
	}
	if got := registryHits.Load(); got != 1 {
		t.Fatalf("accepted suggestion made %d registry checks; want 1", got)
	}
}

func TestAdvisoryReturnsBoundedExplanationAndRegistryVerifiedSuggestion(t *testing.T) {
	var registryHits atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		registryHits.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()
	aiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.Input, "\"explanation\"") || !strings.Contains(payload.Input, "at most 400 characters") {
			t.Fatalf("advisory request did not require bounded structured output: %q", payload.Input)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"output_text":"{\"package\":\"react\",\"explanation\":\"The package name closely resembles the established public dependency.\"}"}`))
	}))
	defer aiServer.Close()
	registry := validator.NewClient()
	registry.Endpoints = validator.Endpoints{PyPI: registryServer.URL, NPM: registryServer.URL}
	client := &Client{HTTPClient: aiServer.Client(), Endpoint: aiServer.URL, APIKey: "test", Model: "test"}
	advisory, err := client.Advise(context.Background(), model.Finding{Name: "reaxt", Ecosystem: model.NPM, Path: "app.js", Line: 1, Snippet: "require('reaxt')"}, registry, pgmath.NewRiskEngine())
	if err != nil {
		t.Fatalf("advise: %v", err)
	}
	if advisory.Suggestion != "react" || advisory.Explanation != "The package name closely resembles the established public dependency." {
		t.Fatalf("advisory = %#v", advisory)
	}
	if got := registryHits.Load(); got != 1 {
		t.Fatalf("accepted advisory made %d registry checks; want 1", got)
	}
}

func TestAdvisoryRejectsMissingAndOversizedExplanation(t *testing.T) {
	finding := model.Finding{Name: "reaxt", Ecosystem: model.NPM}
	for _, answer := range []string{
		`{"package":"react"}`,
		`{"package":"react","explanation":"` + strings.Repeat("a", maxAdvisoryExplanationRunes+1) + `"}`,
	} {
		if _, err := validateAdvisory(context.Background(), answer, finding, nil, nil); err == nil {
			t.Fatalf("invalid advisory output was accepted: %s", answer)
		}
	}
}

func TestNormalizeAdvisoryExplanationRemovesTerminalControls(t *testing.T) {
	got := normalizeAdvisoryExplanation("A concise\x1b[2J explanation\nwith spacing.")
	if got != "A concise[2J explanation with spacing." {
		t.Fatalf("normalized explanation = %q", got)
	}
}

func TestGeminiSuggestionUsesStructuredOutputAndRegistryVerification(t *testing.T) {
	var registryHits atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		registryHits.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()
	geminiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1beta/models/gemini-2.5-flash:generateContent" {
			t.Errorf("unexpected Gemini path %q", request.URL.Path)
		}
		if request.Header.Get("x-goog-api-key") != "gemini-test-key" {
			t.Error("Gemini request omitted its API-key header")
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		config, ok := payload["generationConfig"].(map[string]any)
		if !ok || config["response_mime_type"] != "application/json" || config["response_schema"] == nil {
			t.Fatalf("Gemini request did not require structured JSON: %#v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"candidates": []any{map[string]any{
				"content": map[string]any{
					"parts": []any{map[string]string{"text": `{"package":"react"}`}},
				},
			}},
		})
	}))
	defer geminiServer.Close()
	registry := validator.NewClient()
	registry.Endpoints = validator.Endpoints{PyPI: registryServer.URL, NPM: registryServer.URL}
	client := &Client{
		HTTPClient: geminiServer.Client(),
		Endpoint:   geminiServer.URL + "/v1beta/models",
		Model:      "gemini-2.5-flash",
		APIKey:     "gemini-test-key",
		Provider:   Gemini,
	}
	suggestion, err := client.Suggest(context.Background(), model.Finding{Name: "reaxt", Ecosystem: model.NPM}, registry, pgmath.NewRiskEngine())
	if err != nil || suggestion != "react" {
		t.Fatalf("suggestion=%q err=%v", suggestion, err)
	}
	if got := registryHits.Load(); got != 1 {
		t.Fatalf("accepted Gemini suggestion made %d registry checks; want 1", got)
	}
}

func TestXAIAndAnthropicSuggestionsUseTheirAuthenticatedStructuredFormats(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider Provider
		model    string
		apiKey   string
		endpoint func(string) string
		handler  func(*testing.T, http.ResponseWriter, *http.Request)
	}{
		{
			name:     "xai",
			provider: XAI,
			model:    "grok-4.5",
			apiKey:   "xai-test-key",
			endpoint: func(serverURL string) string { return serverURL },
			handler: func(t *testing.T, writer http.ResponseWriter, request *http.Request) {
				t.Helper()
				if request.Header.Get("Authorization") != "Bearer xai-test-key" {
					t.Error("xAI request omitted its bearer credential")
				}
				var payload map[string]any
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["model"] != "grok-4.5" || payload["input"] == "" {
					t.Fatalf("unexpected xAI request: %#v", payload)
				}
				_ = json.NewEncoder(writer).Encode(map[string]string{"output_text": `{"package":"react"}`})
			},
		},
		{
			name:     "anthropic",
			provider: Anthropic,
			model:    "claude-sonnet-5",
			apiKey:   "anthropic-test-key",
			endpoint: func(serverURL string) string { return serverURL },
			handler: func(t *testing.T, writer http.ResponseWriter, request *http.Request) {
				t.Helper()
				if request.Header.Get("x-api-key") != "anthropic-test-key" {
					t.Error("Anthropic request omitted its API-key credential")
				}
				if request.Header.Get("anthropic-version") != "2023-06-01" {
					t.Error("Anthropic request omitted its API version")
				}
				var payload map[string]any
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				choice, ok := payload["tool_choice"].(map[string]any)
				if !ok || choice["type"] != "tool" || choice["name"] != "suggest_package" || payload["model"] != "claude-sonnet-5" {
					t.Fatalf("Anthropic request did not force the restricted tool: %#v", payload)
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"content": []any{map[string]any{
					"type":  "tool_use",
					"name":  "suggest_package",
					"input": map[string]string{"package": "react"},
				}}})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var registryHits atomic.Int32
			registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				registryHits.Add(1)
				writer.WriteHeader(http.StatusOK)
			}))
			defer registryServer.Close()
			aiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				test.handler(t, writer, request)
			}))
			defer aiServer.Close()
			registry := validator.NewClient()
			registry.Endpoints = validator.Endpoints{PyPI: registryServer.URL, NPM: registryServer.URL}
			client := &Client{
				HTTPClient: aiServer.Client(),
				Endpoint:   test.endpoint(aiServer.URL),
				Model:      test.model,
				APIKey:     test.apiKey,
				Provider:   test.provider,
			}
			suggestion, err := client.Suggest(context.Background(), model.Finding{Name: "reaxt", Ecosystem: model.NPM}, registry, pgmath.NewRiskEngine())
			if err != nil || suggestion != "react" {
				t.Fatalf("suggestion=%q err=%v", suggestion, err)
			}
			if got := registryHits.Load(); got != 1 {
				t.Fatalf("accepted %s suggestion made %d registry checks; want 1", test.provider, got)
			}
		})
	}
}

func TestNewClientWithKeyRejectsUnknownProviderAndUnsafeModel(t *testing.T) {
	if _, err := NewClientWithKey(Provider("other"), "", "key"); err == nil {
		t.Fatal("unknown provider was accepted")
	}
	if _, err := NewClientWithKey(Gemini, "../../unsafe", "key"); err == nil {
		t.Fatal("unsafe model name was accepted")
	}
	if _, err := NewClientWithKey(Azure, "deployment-name", "key"); err == nil {
		t.Fatal("Azure was accepted despite requiring environment-derived endpoint configuration")
	}
}

func TestNewClientWithKeyUsesFixedEndpointsAndDefaultModels(t *testing.T) {
	for _, test := range []struct {
		provider Provider
		want     Provider
		key      string
		model    string
		endpoint string
	}{
		{provider: OpenAI, want: OpenAI, key: "openai-key", model: "gpt-5.6", endpoint: openAIEndpoint},
		{provider: Gemini, want: Gemini, key: "gemini-key", model: "gemini-2.5-flash", endpoint: geminiEndpoint},
		{provider: XAI, want: XAI, key: "xai-key", model: "grok-4.5", endpoint: xAIEndpoint},
		{provider: Anthropic, want: Anthropic, key: "anthropic-key", model: "claude-sonnet-5", endpoint: anthropicEndpoint},
		{provider: "grok", want: XAI, key: "xai-key", model: "grok-4.5", endpoint: xAIEndpoint},
		{provider: "claude", want: Anthropic, key: "anthropic-key", model: "claude-sonnet-5", endpoint: anthropicEndpoint},
	} {
		t.Run(string(test.provider), func(t *testing.T) {
			client, err := NewClientWithKey(test.provider, "", test.key)
			if err != nil {
				t.Fatal(err)
			}
			if client.Provider != test.want || client.APIKey != test.key || client.Model != test.model || client.Endpoint != test.endpoint {
				t.Fatalf("unexpected %s client: %#v", test.provider, client)
			}
		})
	}
}

func TestSuggestionRejectsInvalidNonexistentAndUnsafeAnswers(t *testing.T) {
	for _, test := range []struct {
		name             string
		suggestion       string
		registryStatus   int
		wantRegistryHits int32
	}{
		{name: "invalid token", suggestion: "../../etc", registryStatus: http.StatusOK, wantRegistryHits: 0},
		{name: "nonexistent package", suggestion: "not-real-package", registryStatus: http.StatusNotFound, wantRegistryHits: 1},
		{name: "untrusted typosquat", suggestion: "reactt", registryStatus: http.StatusOK, wantRegistryHits: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var registryHits atomic.Int32
			registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				registryHits.Add(1)
				writer.WriteHeader(test.registryStatus)
			}))
			defer registryServer.Close()
			aiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(writer).Encode(struct {
					OutputText string `json:"output_text"`
				}{OutputText: `{"package":"` + test.suggestion + `"}`})
			}))
			defer aiServer.Close()
			registry := validator.NewClient()
			registry.Endpoints = validator.Endpoints{PyPI: registryServer.URL, NPM: registryServer.URL}
			client := &Client{HTTPClient: aiServer.Client(), Endpoint: aiServer.URL, APIKey: "test", Model: "test"}
			_, err := client.Suggest(context.Background(), model.Finding{Name: "reaxt", Ecosystem: model.NPM}, registry, pgmath.NewRiskEngine())
			if err == nil {
				t.Fatal("unsafe AI suggestion was accepted")
			}
			if got := registryHits.Load(); got != test.wantRegistryHits {
				t.Fatalf("registry checks = %d; want %d", got, test.wantRegistryHits)
			}
		})
	}
}
