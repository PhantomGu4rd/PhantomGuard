package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Setup is a fully resolved user-local AI configuration: an allowlisted
// provider, its credential, and one model confirmed to exist for that key.
type Setup struct {
	Provider Provider
	Key      string
	Model    string
}

// maxModelsShown bounds the interactive model listing so a large account
// catalogue stays readable; the remainder is summarised, never hidden.
const maxModelsShown = 10

const (
	openAIModelsEndpoint     = "https://api.openai.com/v1/models"
	xAIModelsEndpoint        = "https://api.x.ai/v1/models"
	anthropicModelsEndpoint  = "https://api.anthropic.com/v1/models"
	geminiModelsEndpoint     = "https://generativelanguage.googleapis.com/v1beta/models"
	openRouterModelsEndpoint = "https://openrouter.ai/api/v1/models"
)

func modelsEndpointFor(provider Provider) (string, error) {
	switch provider {
	case Gemini:
		return geminiModelsEndpoint, nil
	case XAI:
		return xAIModelsEndpoint, nil
	case Anthropic:
		return anthropicModelsEndpoint, nil
	case OpenRouter:
		return openRouterModelsEndpoint, nil
	default:
		return openAIModelsEndpoint, nil
	}
}

// ModelLister queries one provider's model catalogue. Endpoint is injectable so
// tests can use httptest; production construction always uses the fixed URLs.
type ModelLister struct {
	HTTPClient *http.Client
	Endpoint   string
	Provider   Provider
	Key        string
}

// ListModels fetches the model catalogue for an allowlisted provider, which
// also proves the supplied credential is currently valid.
func ListModels(ctx context.Context, provider Provider, key string) ([]string, error) {
	provider = canonicalProvider(provider)
	if !isSupportedProvider(provider) {
		return nil, fmt.Errorf("unsupported AI provider %q", provider)
	}
	endpoint, err := modelsEndpointFor(provider)
	if err != nil {
		return nil, err
	}
	lister := ModelLister{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Endpoint:   endpoint,
		Provider:   provider,
		Key:        key,
	}
	return lister.List(ctx)
}

// List performs the catalogue request and normalises every provider response
// into a plain, sorted model-name slice.
func (l ModelLister) List(ctx context.Context) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, l.Endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build %s models request: %w", l.Provider, err)
	}
	switch canonicalProvider(l.Provider) {
	case Anthropic:
		request.Header.Set("x-api-key", l.Key)
		request.Header.Set("anthropic-version", "2023-06-01")
	case Gemini:
		request.Header.Set("x-goog-api-key", l.Key)
	default:
		request.Header.Set("Authorization", "Bearer "+l.Key)
	}
	response, err := l.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s models: %w", l.Provider, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if detail := apiErrorDetail(response.Body); detail != "" {
			return nil, fmt.Errorf("%s models endpoint returned %s: %s", l.Provider, response.Status, detail)
		}
		return nil, fmt.Errorf("%s models endpoint returned %s", l.Provider, response.Status)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode %s models response: %w", l.Provider, err)
	}
	var models []string
	for _, item := range body.Data {
		if name := strings.TrimSpace(item.ID); name != "" {
			models = append(models, name)
		}
	}
	for _, item := range body.Models {
		if name := strings.TrimSpace(strings.TrimPrefix(item.Name, "models/")); name != "" {
			models = append(models, name)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%s returned no models for this key", l.Provider)
	}
	sort.Strings(models)
	return models, nil
}

// apiErrorDetail extracts the provider's human-readable error message from a
// bounded read of an error response body, e.g. billing or model-access causes.
func apiErrorDetail(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, 2048))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return ""
	}
	return strings.Join(strings.Fields(envelope.Error.Message), " ")
}

// ResolveSetup collects and validates a local advisory configuration. The key
// is proven by listing the account's models (at most maxModelsShown displayed,
// with a visible remainder count), and the user confirms one model.
func ResolveSetup(in io.Reader, out io.Writer, keys map[Provider]string, defaultProvider Provider, list func(context.Context, Provider, string) ([]string, error)) (Setup, error) {
	reader := bufio.NewReader(in)
	provider, key, err := resolveProviderAndKey(reader, out, keys, defaultProvider)
	if err != nil {
		return Setup{}, err
	}
	models, err := list(context.Background(), provider, key)
	if err != nil {
		return Setup{}, err
	}
	model, err := chooseModel(reader, out, provider, models)
	if err != nil {
		return Setup{}, err
	}
	fmt.Fprintf(out, "AI advisory credentials verified: %s · %s\n", provider, model)
	return Setup{Provider: provider, Key: key, Model: model}, nil
}

func resolveProviderAndKey(reader *bufio.Reader, out io.Writer, keys map[Provider]string, defaultProvider Provider) (Provider, string, error) {
	supplied := make([]Provider, 0, len(keys))
	for provider, key := range keys {
		if strings.TrimSpace(key) != "" {
			supplied = append(supplied, canonicalProvider(provider))
		}
	}
	if len(supplied) > 1 {
		return "", "", fmt.Errorf("provide at most one provider key flag")
	}
	if len(supplied) == 1 {
		provider := supplied[0]
		return provider, strings.TrimSpace(keys[provider]), nil
	}
	provider, err := promptProvider(reader, out, canonicalProvider(defaultProvider))
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(out, "Enter your %s API key (input is visible; it will be stored only in your local AI configuration): ", provider)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", "", fmt.Errorf("read API key: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return "", "", fmt.Errorf("an API key is required to enable AI suggestions")
	}
	return provider, key, nil
}

func promptProvider(reader *bufio.Reader, out io.Writer, defaultProvider Provider) (Provider, error) {
	ordered := []Provider{OpenAI, Anthropic, Gemini, XAI, OpenRouter}
	if !isSupportedProvider(defaultProvider) {
		defaultProvider = OpenAI
	}
	fmt.Fprintln(out, "Select an AI provider:")
	for index, provider := range ordered {
		marker := " "
		if provider == defaultProvider {
			marker = "*"
		}
		fmt.Fprintf(out, "  %d. %-10s %s\n", index+1, provider, marker)
	}
	fmt.Fprintf(out, "Provider [1-%d, Enter = %s]: ", len(ordered), defaultProvider)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read provider choice: %w", err)
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	if choice == "" {
		return defaultProvider, nil
	}
	if index, err := strconv.Atoi(choice); err == nil {
		if index < 1 || index > len(ordered) {
			return "", fmt.Errorf("provider choice must be between 1 and %d", len(ordered))
		}
		return ordered[index-1], nil
	}
	provider := canonicalProvider(Provider(choice))
	if !isSupportedProvider(provider) {
		return "", fmt.Errorf("unsupported AI provider %q", choice)
	}
	return provider, nil
}

func chooseModel(reader *bufio.Reader, out io.Writer, provider Provider, models []string) (string, error) {
	recommended := recommendedModel(provider, models)
	shown := models
	if len(shown) > maxModelsShown {
		shown = shown[:maxModelsShown]
	}
	fmt.Fprintf(out, "Available %s models (%d found):\n", provider, len(models))
	for index, model := range shown {
		marker := ""
		if model == recommended {
			marker = "  (recommended)"
		}
		fmt.Fprintf(out, "  %d. %s%s\n", index+1, model, marker)
	}
	if remaining := len(models) - len(shown); remaining > 0 {
		fmt.Fprintf(out, "  … and %d more available\n", remaining)
	}
	fmt.Fprintf(out, "Model [1-%d or full name, Enter = %s]: ", len(shown), recommended)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read model choice: %w", err)
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return recommended, nil
	}
	if index, err := strconv.Atoi(choice); err == nil {
		if index < 1 || index > len(shown) {
			return "", fmt.Errorf("model choice must be between 1 and %d", len(shown))
		}
		return shown[index-1], nil
	}
	for _, model := range models {
		if strings.EqualFold(model, choice) {
			return model, nil
		}
	}
	return "", fmt.Errorf("model %q is not available for this key", choice)
}

// recommendedModel prefers the provider's bundled default when the account can
// use it, so Enter always selects something proven to exist.
func recommendedModel(provider Provider, models []string) string {
	fallback, err := resolvedModel(provider, "")
	if err == nil {
		for _, model := range models {
			if strings.EqualFold(model, fallback) {
				return model
			}
		}
	}
	return models[0]
}
