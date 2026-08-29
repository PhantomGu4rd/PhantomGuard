package ai

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func stubLister(models []string, err error) func(context.Context, Provider, string) ([]string, error) {
	return func(context.Context, Provider, string) ([]string, error) {
		return models, err
	}
}

func TestResolveSetupUsesSuppliedKeyFlagWithoutPrompting(t *testing.T) {
	var out bytes.Buffer
	models := []string{
		"claude-sonnet-5", "m-two", "m-three", "m-four", "m-five", "m-six",
		"m-seven", "m-eight", "m-nine", "m-ten", "m-eleven", "m-twelve",
	}
	setup, err := ResolveSetup(strings.NewReader("\n"), &out, map[Provider]string{Anthropic: "sk-test"}, OpenAI, stubLister(models, nil))
	if err != nil {
		t.Fatalf("resolve setup: %v", err)
	}
	if setup.Provider != Anthropic || setup.Key != "sk-test" {
		t.Fatalf("key flag did not select its provider: %+v", setup)
	}
	if setup.Model != "claude-sonnet-5" {
		t.Fatalf("Enter did not select the recommended default model: %q", setup.Model)
	}
	if strings.Contains(out.String(), "Select an AI provider") {
		t.Fatal("provider prompt appeared despite an explicit key flag")
	}
	if !strings.Contains(out.String(), "12 found") {
		t.Fatalf("model count missing from output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "… and 2 more available") {
		t.Fatalf("hidden model remainder was not announced:\n%s", out.String())
	}
	if strings.Contains(out.String(), "m-eleven") || strings.Contains(out.String(), "m-twelve") {
		t.Fatal("more than maxModelsShown models were displayed")
	}
}

func TestResolveSetupPromptsForProviderAndKey(t *testing.T) {
	var out bytes.Buffer
	input := strings.NewReader("2\nsk-interactive\n3\n")
	models := []string{"alpha", "beta", "claude-sonnet-5"}
	setup, err := ResolveSetup(input, &out, map[Provider]string{}, OpenAI, stubLister(models, nil))
	if err != nil {
		t.Fatalf("resolve setup: %v", err)
	}
	if setup.Provider != Anthropic {
		t.Fatalf("menu option 2 should select anthropic, got %s", setup.Provider)
	}
	if setup.Key != "sk-interactive" {
		t.Fatalf("interactive key was not captured: %q", setup.Key)
	}
	if setup.Model != "claude-sonnet-5" {
		t.Fatalf("numeric model choice failed: %q", setup.Model)
	}
	if !strings.Contains(out.String(), "Select an AI provider") {
		t.Fatal("provider prompt was not shown")
	}
	if !strings.Contains(out.String(), "(recommended)") {
		t.Fatal("recommended model marker missing")
	}
}

func TestResolveSetupRejectsBadInput(t *testing.T) {
	if _, err := ResolveSetup(strings.NewReader("\n"), &bytes.Buffer{}, map[Provider]string{Anthropic: "a", OpenAI: "b"}, OpenAI, stubLister([]string{"m"}, nil)); err == nil {
		t.Fatal("two key flags were accepted")
	}
	if _, err := ResolveSetup(strings.NewReader("1\n\n"), &bytes.Buffer{}, map[Provider]string{}, OpenAI, stubLister([]string{"m"}, nil)); err == nil {
		t.Fatal("an empty API key was accepted")
	}
	if _, err := ResolveSetup(strings.NewReader("9\n"), &bytes.Buffer{}, map[Provider]string{}, OpenAI, stubLister([]string{"m"}, nil)); err == nil {
		t.Fatal("an out-of-range provider number was accepted")
	}
	if _, err := ResolveSetup(strings.NewReader("no-such-model\n"), &bytes.Buffer{}, map[Provider]string{OpenAI: "k"}, OpenAI, stubLister([]string{"m"}, nil)); err == nil {
		t.Fatal("an unavailable model name was accepted")
	}
	if _, err := ResolveSetup(strings.NewReader("\n"), &bytes.Buffer{}, map[Provider]string{OpenAI: "k"}, OpenAI, stubLister(nil, fmt.Errorf("invalid key"))); err == nil {
		t.Fatal("a failed model listing did not abort activation")
	}
}

func TestPromptProviderDoesNotOfferEnvironmentConfiguredAzure(t *testing.T) {
	var out bytes.Buffer
	if _, err := promptProvider(bufio.NewReader(strings.NewReader("6\n")), &out, OpenAI); err == nil {
		t.Fatal("removed Azure menu choice was accepted")
	}
	if strings.Contains(strings.ToLower(out.String()), "azure") {
		t.Fatalf("Azure remained in advisory provider menu:\n%s", out.String())
	}
}

func TestModelListerParsesProviderCatalogues(t *testing.T) {
	openAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-o" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"gpt-b"},{"id":"gpt-a"}]}`)
	}))
	defer openAIServer.Close()
	models, err := (ModelLister{HTTPClient: openAIServer.Client(), Endpoint: openAIServer.URL, Provider: OpenAI, Key: "sk-o"}).List(context.Background())
	if err != nil {
		t.Fatalf("openai list: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-a" || models[1] != "gpt-b" {
		t.Fatalf("openai catalogue was not parsed and sorted: %v", models)
	}

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "sk-g" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"models":[{"name":"models/gemini-x"}]}`)
	}))
	defer geminiServer.Close()
	models, err = (ModelLister{HTTPClient: geminiServer.Client(), Endpoint: geminiServer.URL, Provider: Gemini, Key: "sk-g"}).List(context.Background())
	if err != nil {
		t.Fatalf("gemini list: %v", err)
	}
	if len(models) != 1 || models[0] != "gemini-x" {
		t.Fatalf("gemini model prefix was not stripped: %v", models)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"credit balance is too low"}}`)
	}))
	defer errorServer.Close()
	_, err = (ModelLister{HTTPClient: errorServer.Client(), Endpoint: errorServer.URL, Provider: Anthropic, Key: "sk-a"}).List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "credit balance is too low") {
		t.Fatalf("provider error detail was not surfaced: %v", err)
	}
}
