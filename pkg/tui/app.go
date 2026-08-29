package tui

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/phantomguard/phantomguard/pkg/buildinfo"
	"github.com/phantomguard/phantomguard/pkg/model"
)

// Options adapts the TUI to a binary, tests, and redirected streams.
type Options struct {
	Root        string
	Args        []string
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Width       int
	Interactive *bool
	Backend     Backend
	Theme       Theme
	Version     string
	Build       string
}

// Run parses TUI-only options, renders the splash, and starts the line-based
// terminal workspace when both standard streams are terminals. It returns a
// conventional process exit code, never calling os.Exit itself.
func Run(options Options) int {
	options = normaliseOptions(options)
	noColor, noInteractive, help, err := parseArgs(options.Args)
	if err != nil {
		fmt.Fprintln(options.Err, "PhantomGuard TUI:", err)
		printUsage(options.Err)
		return 2
	}
	if help {
		printUsage(options.Out)
		return 0
	}
	if options.Backend == nil {
		backend, err := NewScannerBackend(options.Root)
		if err != nil {
			fmt.Fprintln(options.Err, "PhantomGuard TUI:", err)
			return 2
		}
		options.Backend = backend
	}

	status, err := options.Backend.Status(context.Background())
	if err != nil {
		fmt.Fprintln(options.Err, "PhantomGuard TUI status:", err)
		return 2
	}
	interactive := isInteractive(options, noInteractive)
	color := !noColor && colourAllowed(options.Out)
	RenderWelcome(options.Out, options.Width, color, options.Theme, WelcomeModel{
		Status: status, Version: options.Version, Build: options.Build,
	})
	if !interactive {
		fmt.Fprintln(options.Out)
		message := "  Run from an interactive terminal to enter commands. Use --help for non-interactive options."
		if noInteractive {
			message = "  Welcome screen rendered without an interactive prompt."
		}
		fmt.Fprintln(options.Out, fit(message, usableWidth(options.Width)))
		return 0
	}

	fmt.Fprintln(options.Out)
	fmt.Fprintln(options.Out, newStyle(color, options.Theme).muted(fit("  "+options.Theme.PromptHint, usableWidth(options.Width))))
	return commandLoop(options, color, status)
}

func normaliseOptions(options Options) Options {
	if options.In == nil {
		options.In = os.Stdin
	}
	if options.Out == nil {
		options.Out = os.Stdout
	}
	if options.Err == nil {
		options.Err = os.Stderr
	}
	if options.Width == 0 {
		options.Width = terminalWidth()
	}
	if options.Theme.Name == "" {
		options.Theme = DefaultTheme
	}
	if options.Version == "" {
		options.Version = buildinfo.Version
	}
	if options.Build == "" {
		options.Build = currentCommit(options.Root)
	}
	return options
}

func parseArgs(arguments []string) (noColor, noInteractive, help bool, err error) {
	for _, argument := range arguments {
		switch argument {
		case "--no-color":
			noColor = true
		case "--no-interactive":
			noInteractive = true
		case "--help", "-h", "help":
			help = true
		default:
			return false, false, false, fmt.Errorf("unknown option %q", argument)
		}
	}
	return noColor, noInteractive, help, nil
}

func colourAllowed(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && file != nil && isTerminalFile(file)
}

func isTerminalFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func isInteractive(options Options, noInteractive bool) bool {
	if noInteractive {
		return false
	}
	if options.Interactive != nil {
		return *options.Interactive
	}
	in, inOK := options.In.(*os.File)
	out, outOK := options.Out.(*os.File)
	return inOK && outOK && isTerminalFile(in) && isTerminalFile(out)
}

func commandLoop(options Options, color bool, status Status) int {
	input := bufio.NewReader(options.In)
	for {
		fmt.Fprint(options.Out, newStyle(color, options.Theme).accentText("  "+options.Theme.Prompt)+" ")
		line, err := input.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(options.Err, "PhantomGuard TUI input:", err)
				return 2
			}
			if strings.TrimSpace(line) != "" {
				if exit := dispatchCommand(options, color, status, input, line); exit {
					return 0
				}
			}
			fmt.Fprintln(options.Out)
			return 0
		}
		if dispatchCommand(options, color, status, input, line) {
			return 0
		}
	}
}

func dispatchCommand(options Options, color bool, status Status, input *bufio.Reader, raw string) bool {
	command := strings.TrimSpace(raw)
	if command == "" {
		return false
	}
	parts, err := splitCommand(command)
	if err != nil {
		fmt.Fprintln(options.Out, "  Command error:", err)
		return false
	}
	if len(parts) == 0 {
		return false
	}
	name := strings.TrimPrefix(strings.ToLower(parts[0]), "/")
	arguments := parts[1:]
	switch name {
	case "quit", "exit":
		fmt.Fprintln(options.Out, newStyle(color, options.Theme).accentText("  PhantomGuard workspace closed. Stay sharp."))
		return true
	case "help":
		printCommandHelp(options.Out, color, options.Theme, options.Width)
	case "status", "policy":
		printStatus(options.Out, color, options.Theme, options.Width, status)
	case "scan":
		request, err := parseScanRequest(arguments)
		if err != nil {
			fmt.Fprintln(options.Out, "  Scan command error:", err)
			return false
		}
		runScan(options, color, request)
	case "cache":
		runCache(options, color, arguments)
	case "install":
		runInstall(options, color, arguments)
	case "fix":
		request, err := parseFixRequest(arguments)
		if err != nil {
			fmt.Fprintln(options.Out, "  Fix command error:", err)
			return false
		}
		runFix(options, color, input, request)
	case "about", "version":
		fmt.Fprintln(options.Out, fit(fmt.Sprintf("  %s %s · build %s", options.Theme.Name, options.Version, options.Build), usableWidth(options.Width)))
	default:
		fmt.Fprintf(options.Out, "  Unknown command %q. Type help for the command list.\n", parts[0])
	}
	return false
}

func runScan(options Options, color bool, request ScanRequest) {
	s := newStyle(color, options.Theme)
	label := "staged changes"
	switch request.Target {
	case TargetAll:
		label = "all supported files"
	case TargetPaths:
		label = strings.Join(request.Paths, ", ")
	}
	fmt.Fprintln(options.Out, s.muted(fit("  Scanning "+label+" through the PhantomGuard pipeline…", usableWidth(options.Width))))
	result, err := options.Backend.Scan(context.Background(), request)
	if err != nil {
		fmt.Fprintln(options.Out, "  Scan failed:", err)
		return
	}
	if result.Skipped {
		fmt.Fprintln(options.Out, s.accentText("  SKIPPED  PHANTOMGUARD_SKIP=1 is set; no files were scanned."))
		return
	}
	renderScanResult(options.Out, color, options.Theme, options.Width, request.Target, result)
}

func runCache(options Options, color bool, arguments []string) {
	s := newStyle(color, options.Theme)
	if len(arguments) == 1 && strings.EqualFold(arguments[0], "clear") {
		if err := options.Backend.ClearCache(context.Background()); err != nil {
			fmt.Fprintln(options.Out, "  Cache clear failed:", err)
			return
		}
		fmt.Fprintln(options.Out, s.accentText("  Cache cleared. Future scans will re-check definitive registry answers."))
		return
	}
	if len(arguments) != 0 && !(len(arguments) == 1 && strings.EqualFold(arguments[0], "status")) {
		fmt.Fprintln(options.Out, "  Cache command error: use cache, cache status, or cache clear")
		return
	}
	status, err := options.Backend.CacheStatus(context.Background())
	if err != nil {
		fmt.Fprintln(options.Out, "  Cache status failed:", err)
		return
	}
	fmt.Fprintln(options.Out, s.accentText("  CACHE"))
	width := usableWidth(options.Width)
	fmt.Fprintln(options.Out, fit(fmt.Sprintf("  %d entries · %d verified · %d confirmed phantom", status.Entries, status.Verified, status.Phantom), width))
	fmt.Fprintln(options.Out, fit(fmt.Sprintf("  retention: verified %dh · phantom %dh · unknown results are never cached", status.PositiveTTLHours, status.NegativeTTLHours), width))
}

func runInstall(options Options, color bool, arguments []string) {
	force := len(arguments) == 1 && arguments[0] == "--force"
	if len(arguments) != 0 && !force {
		fmt.Fprintln(options.Out, "  Install command error: use install or install --force")
		return
	}
	hook, err := options.Backend.Install(context.Background(), force)
	if err != nil {
		fmt.Fprintln(options.Out, "  Hook installation failed:", err)
		return
	}
	fmt.Fprintln(options.Out, newStyle(color, options.Theme).accentText(fit("  Installed PhantomGuard hook at "+hook, usableWidth(options.Width))))
}

func runFix(options Options, color bool, input *bufio.Reader, request FixRequest) {
	fmt.Fprintln(options.Out, newStyle(color, options.Theme).muted("  Verifying replacement and preparing a diff. The fix will require a y confirmation."))
	if err := options.Backend.Fix(context.Background(), request, input, options.Out); err != nil {
		fmt.Fprintln(options.Out, "  Fix refused:", err)
	}
}

func parseScanRequest(arguments []string) (ScanRequest, error) {
	request := ScanRequest{Target: TargetStaged}
	var values []string
	for _, argument := range arguments {
		if argument == "--strict" {
			request.Strict = true
			continue
		}
		if strings.HasPrefix(argument, "--") {
			return ScanRequest{}, fmt.Errorf("unknown option %q; use --strict", argument)
		}
		values = append(values, argument)
	}
	if len(values) == 0 {
		return request, nil
	}
	switch values[0] {
	case "staged":
		if len(values) != 1 {
			return ScanRequest{}, fmt.Errorf("scan staged accepts no paths")
		}
		return request, nil
	case "all":
		if len(values) != 1 {
			return ScanRequest{}, fmt.Errorf("scan all accepts no paths")
		}
		request.Target = TargetAll
		return request, nil
	default:
		request.Target = TargetPaths
		request.Paths = values
		return request, nil
	}
}

func parseFixRequest(arguments []string) (FixRequest, error) {
	flags := flag.NewFlagSet("fix", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("file", "", "repository-relative file to edit")
	from := flags.String("from", "", "phantom package string to replace")
	to := flags.String("to", "", "registry-verified replacement")
	ecosystem := flags.String("ecosystem", "", "pypi or npm")
	if err := flags.Parse(arguments); err != nil {
		return FixRequest{}, err
	}
	if flags.NArg() != 0 {
		return FixRequest{}, fmt.Errorf("fix accepts flags only")
	}
	if *path == "" || *from == "" || *to == "" || *ecosystem == "" {
		return FixRequest{}, fmt.Errorf("use fix --file <path> --from <package> --to <verified-package> --ecosystem <pypi|npm>")
	}
	return FixRequest{Path: *path, From: *from, To: *to, Ecosystem: model.Ecosystem(strings.ToLower(*ecosystem))}, nil
}

// splitCommand keeps the line interface friendly for repository paths with
// spaces while deliberately avoiding shell expansion or command execution.
func splitCommand(value string) ([]string, error) {
	var values []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			values = append(values, current.String())
			current.Reset()
		}
	}
	for _, character := range value {
		if escaped {
			current.WriteRune(character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				current.WriteRune(character)
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case ' ', '\t':
			flush()
		default:
			current.WriteRune(character)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted value")
	}
	flush()
	return values, nil
}

func printStatus(out io.Writer, color bool, theme Theme, width int, status Status) {
	s := newStyle(color, theme)
	width = usableWidth(width)
	fmt.Fprintln(out, s.accentText("  ●")+" "+fit("Pipeline: connected locally; no code is executed", width-4))
	fmt.Fprintln(out, s.accentText("  ●")+" "+fit("Repository: "+status.Repository+" ["+status.Branch+"]", width-4))
	fmt.Fprintln(out, s.accentText("  ●")+" "+fit("Policy: "+status.FailMode+" · PyPI + npm registries", width-4))
}

func printCommandHelp(out io.Writer, color bool, theme Theme, width int) {
	s := newStyle(color, theme)
	width = usableWidth(width)
	fmt.Fprintln(out, s.accentText("  COMMANDS"))
	for _, line := range []string{
		"  scan [staged|all|<paths>] [--strict]  Scan the chosen scope.",
		"  status        Show the repository, branch, and active policy.",
		"  cache         Show local cache summary and retention.",
		"  cache clear   Remove cached registry results.",
		"  install [--force]  Install the pre-commit hook; --force chains a foreign hook.",
		"  fix --file <path> --from <name> --to <name> --ecosystem <pypi|npm>",
		"                Verify, preview, and confirm one replacement.",
		"  version       Show the TUI build metadata.",
		"  help          Show this command list.",
		"  quit          Leave the terminal workspace.",
	} {
		fmt.Fprintln(out, fit(line, width))
	}
}

func renderScanResult(out io.Writer, color bool, theme Theme, width int, target Target, result ScanResult) {
	s := newStyle(color, theme)
	width = usableWidth(width)
	if target == TargetAll {
		fmt.Fprintln(out, s.accentText("  FULL-REPOSITORY SCAN"))
	} else if target == TargetPaths {
		fmt.Fprintln(out, s.accentText("  SELECTED-FILES SCAN"))
	} else {
		fmt.Fprintln(out, s.accentText("  STAGED-CHANGES SCAN"))
	}
	confirmed := 0
	verified := 0
	unknown := 0
	for _, finding := range result.Report.Results {
		switch finding.Status {
		case model.Phantom, model.Suspicious:
			confirmed++
		case model.Exists:
			verified++
		case model.Unknown:
			unknown++
		}
	}
	summary := fmt.Sprintf("  %d candidate%s · %d confirmed risk%s · %d unknown · %d verified · %s",
		len(result.Report.Results), plural(len(result.Report.Results)), confirmed, plural(confirmed), unknown, verified, durationLabel(result.Duration))
	fmt.Fprintln(out, s.muted(fit(summary, width)))
	fmt.Fprintln(out)
	if len(result.Report.Results) > 0 {
		indexWidth := len(fmt.Sprintf("%d", len(result.Report.Results)))
		locationWidth := len("FILE:LINE")
		statusWidth := len("STATUS")
		packageWidth := len("PACKAGE")
		for _, finding := range result.Report.Results {
			location := fmt.Sprintf("%s:%d", finding.Finding.Path, finding.Finding.Line)
			locationWidth = max(locationWidth, len(location))
			statusWidth = max(statusWidth, len(finding.Status))
			packageWidth = max(packageWidth, len(finding.Package))
		}
		header := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s", indexWidth, "#", locationWidth, "FILE:LINE", statusWidth, "STATUS", packageWidth, "PACKAGE", "REGISTRY")
		fmt.Fprintln(out, fit(header, width))
		fmt.Fprintln(out, fit("  "+strings.Repeat("─", max(1, min(width-4, 72))), width))
		for index, finding := range result.Report.Results {
			location := fmt.Sprintf("%s:%d", finding.Finding.Path, finding.Finding.Line)
			line := fmt.Sprintf("  %-*d  %-*s  %-*s  %-*s  %s", indexWidth, index+1, locationWidth, location, statusWidth, strings.ToUpper(string(finding.Status)), packageWidth, finding.Package, finding.Finding.Ecosystem)
			fmt.Fprintln(out, fit(line, width))
			if finding.RiskMatch != "" {
				fmt.Fprintln(out, s.accentText(fit("    high-risk typosquat: resembles "+finding.RiskMatch, width)))
			}
			if finding.Provenance != "" {
				fmt.Fprintln(out, s.muted(fit("    provenance: "+finding.Provenance, width)))
			}
			if len(finding.Suggestions) > 0 {
				fmt.Fprintln(out, s.muted(fit("    verified suggestions: "+strings.Join(finding.Suggestions, ", "), width)))
			}
		}
	} else {
		fmt.Fprintln(out, fit("  "+strings.Repeat("─", max(1, min(width-4, 72))), width))
	}
	for _, notice := range result.Report.Notices {
		fmt.Fprintln(out, s.muted(fit("  notice: "+notice, width)))
	}
	if result.Report.Unscannable > 0 {
		fmt.Fprintln(out, s.muted(fmt.Sprintf("  notice: %d dynamic import(s) could not be statically resolved.", result.Report.Unscannable)))
	}
	if result.Blocked {
		fmt.Fprintln(out, s.accentText("  BLOCKED  Resolve confirmed phantom or unsafe dependencies before committing."))
		return
	}
	message := "  PASS  No policy block."
	if unknown > 0 && result.FailMode != "strict" {
		message = "  PASS WITH REVIEW  Registry results remain unknown under warn policy."
	}
	fmt.Fprintln(out, s.accentText(message))
}

// wrapLines word-wraps one message so the AI assist block is never truncated;
// a word longer than the width is hard-split rather than dropped.
func wrapLines(text string, width int) []string {
	if width < 8 {
		width = 8
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		for len([]rune(word)) > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			runes := []rune(word)
			lines = append(lines, string(runes[:width]))
			word = string(runes[width:])
		}
		switch {
		case current == "":
			current = word
		case len([]rune(current))+1+len([]rune(word)) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func durationLabel(duration time.Duration) string {
	if duration < time.Millisecond {
		return "<1ms"
	}
	return duration.Round(time.Millisecond).String()
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: phantomguard tui [--no-color] [--no-interactive]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Starts PhantomGuard's terminal workspace in the current Git repository.")
	fmt.Fprintln(out, "--no-color       Disable ANSI styling (NO_COLOR and TERM=dumb do this automatically).")
	fmt.Fprintln(out, "--no-interactive Render the responsive welcome screen and exit.")
}

// Compile-time assertion helps keep an accidental nil interface from hiding a
// production backend wiring regression.
var _ Backend = (*ScannerBackend)(nil)
