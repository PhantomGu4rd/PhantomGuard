package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/ai"
	pgmath "github.com/phantomguard/phantomguard/pkg/math"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

// aiCommand is the advisory plane. It is intentionally separate from verify,
// scan, TUI, and hook enforcement paths.
func aiCommand(root string, arguments []string) int {
	if len(arguments) == 0 {
		aiUsage(os.Stderr)
		return 2
	}
	switch arguments[0] {
	case "setup":
		return aiSetupCommand(arguments[1:])
	case "explain":
		return aiExplainCommand(root, arguments[1:])
	case "--help", "help", "-h":
		aiUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "PhantomGuard: unknown AI command %q\n", arguments[0])
		aiUsage(os.Stderr)
		return 2
	}
}

// aiSetupCommand writes a user-local 0600 advisory configuration. It is the
// sole command that may persist an AI credential.
func aiSetupCommand(arguments []string) int {
	if len(arguments) != 0 {
		fmt.Fprintln(os.Stderr, "PhantomGuard: ai setup accepts no arguments")
		return 2
	}
	setup, err := ai.ResolveSetup(os.Stdin, os.Stdout, map[ai.Provider]string{}, ai.OpenAI, ai.ListModels)
	if err != nil {
		return commandError(err, "AI setup")
	}
	if err := ai.SaveLocalConfig("", ai.LocalConfig{Provider: setup.Provider, Model: setup.Model, APIKey: setup.Key}); err != nil {
		return commandError(err, "AI setup")
	}
	path, err := ai.DefaultConfigPath()
	if err != nil {
		return commandError(err, "AI setup")
	}
	fmt.Printf("AI advisory configuration saved locally at %s. It is never used by verify or the Git hook.\n", path)
	return 0
}

// aiExplainCommand reruns deterministic staged verification, selects a known
// phantom finding, and then asks the explicitly configured AI for a bounded
// advisory explanation plus a verified suggestion. Its output never changes
// the deterministic scan policy.
func aiExplainCommand(root string, arguments []string) int {
	requested, wantEcosystem, err := parseAIExplainArguments(arguments)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PhantomGuard:", err)
		return 2
	}

	scan, _, err := stagedDeterministicReport(context.Background(), root, false)
	if err != nil {
		return commandError(err, "AI explain deterministic scan")
	}
	result, err := selectAIExplainFinding(scan, requested, wantEcosystem)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PhantomGuard:", err)
		return 2
	}

	local, err := ai.LoadLocalConfig("")
	if err != nil {
		return commandError(err, "AI explain")
	}
	client, err := local.Client()
	if err != nil {
		return commandError(err, "AI explain")
	}
	advisory, err := client.Advise(context.Background(), result.Finding, validator.NewClient(), pgmath.NewRiskEngine())
	if err != nil {
		return commandError(err, "AI explain")
	}
	fmt.Printf("%s:%d  %s  %s (%s)\n", result.Finding.Path, result.Finding.Line, strings.ToUpper(string(result.Status)), result.Package, result.Finding.Ecosystem)
	fmt.Println("Advisory AI Explanation: Not used to determine this result.")
	fmt.Printf("%s\n", advisory.Explanation)
	fmt.Printf("The deterministic registry check returned 404. The independently verified AI suggestion is: %s\n", advisory.Suggestion)
	return 0
}

// selectAIExplainFinding collapses repeated references to the same public
// package in one ecosystem, while preserving the ambiguity protection when a
// package name is genuinely shared by PyPI and npm.
func selectAIExplainFinding(scan model.Report, requested string, wantEcosystem model.Ecosystem) (model.Result, error) {
	type candidateKey struct {
		ecosystem model.Ecosystem
		packageID string
	}
	matches := make(map[candidateKey]model.Result)
	for _, result := range scan.Results {
		if result.Status != model.Phantom || result.Finding.Ecosystem == model.Go {
			continue
		}
		if wantEcosystem != "" && result.Finding.Ecosystem != wantEcosystem {
			continue
		}
		if !strings.EqualFold(requested, result.Package) && !strings.EqualFold(requested, result.Finding.Name) {
			continue
		}
		key := candidateKey{ecosystem: result.Finding.Ecosystem, packageID: strings.ToLower(result.Package)}
		if _, present := matches[key]; !present {
			matches[key] = result
		}
	}
	if len(matches) == 0 {
		return model.Result{}, fmt.Errorf("no confirmed staged phantom dependency named %q. Run phantomguard verify --strict first", requested)
	}
	if len(matches) > 1 {
		return model.Result{}, fmt.Errorf("package is ambiguous; rerun with --ecosystem pypi or --ecosystem npm")
	}
	for _, result := range matches {
		return result, nil
	}
	return model.Result{}, fmt.Errorf("no confirmed staged phantom dependency named %q. Run phantomguard verify --strict first", requested)
}

// parseAIExplainArguments permits the natural documented form
// `ai explain package --ecosystem pypi` as well as flags before the package.
func parseAIExplainArguments(arguments []string) (string, model.Ecosystem, error) {
	var requested string
	var ecosystem string
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--ecosystem":
			if index+1 >= len(arguments) {
				return "", "", fmt.Errorf("--ecosystem requires pypi or npm")
			}
			index++
			ecosystem = arguments[index]
		case strings.HasPrefix(argument, "--ecosystem="):
			ecosystem = strings.TrimPrefix(argument, "--ecosystem=")
		case strings.HasPrefix(argument, "-"):
			return "", "", fmt.Errorf("unknown ai explain option %q", argument)
		case requested == "":
			requested = argument
		default:
			return "", "", fmt.Errorf("ai explain requires exactly one package name")
		}
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", "", fmt.Errorf("ai explain requires one package name")
	}
	wantEcosystem := model.Ecosystem(strings.ToLower(strings.TrimSpace(ecosystem)))
	if wantEcosystem != "" && wantEcosystem != model.PyPI && wantEcosystem != model.NPM {
		return "", "", fmt.Errorf("--ecosystem must be pypi or npm")
	}
	return requested, wantEcosystem, nil
}

func aiUsage(output *os.File) {
	fmt.Fprintln(output, "Usage: phantomguard ai setup | phantomguard ai explain <package> [--ecosystem pypi|npm]")
}
