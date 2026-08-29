package tui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	defaultWidth = 80
	minimumWidth = 12
)

type style struct {
	enabled bool
	accent  string
}

func newStyle(enabled bool, theme Theme) style {
	if !enabled {
		return style{}
	}
	return style{enabled: true, accent: fmt.Sprintf("\x1b[38;2;%d;%d;%dm", theme.AccentRGB[0], theme.AccentRGB[1], theme.AccentRGB[2])}
}

func (s style) accentText(value string) string {
	if !s.enabled {
		return value
	}
	return s.accent + value + "\x1b[0m"
}

func (s style) muted(value string) string {
	if !s.enabled {
		return value
	}
	return "\x1b[2m" + value + "\x1b[0m"
}

func (s style) wordmark(value string) string {
	if !s.enabled {
		return value
	}
	var output strings.Builder
	for _, character := range value {
		switch character {
		case '█':
			output.WriteString(s.accent)
			output.WriteRune(character)
			output.WriteString("\x1b[0m")
		case '░':
			output.WriteString("\x1b[2;" + strings.TrimPrefix(s.accent, "\x1b["))
			output.WriteRune(character)
			output.WriteString("\x1b[0m")
		default:
			output.WriteRune(character)
		}
	}
	return output.String()
}

// WelcomeModel is the dynamic, real repository data used in the splash.
type WelcomeModel struct {
	Status  Status
	Version string
	Build   string
}

// RenderWelcome renders a width-safe splash. It is presentation-only; real
// scanning happens only after a user explicitly enters a scan command.
func RenderWelcome(out io.Writer, width int, color bool, theme Theme, model WelcomeModel) {
	width = usableWidth(width)
	s := newStyle(color, theme)
	if width < 64 {
		renderCompactWelcome(out, width, s, theme, model)
		return
	}

	boxTop(out, width, s)
	boxContent(out, width, s, theme.WelcomePrefix+" "+theme.Name+" — "+model.Version)
	boxContent(out, width, s, "Local scanner ready. Type help to see commands.")
	boxBottom(out, width, s)
	fmt.Fprintln(out)

	for _, row := range wordmarkRows(color) {
		fmt.Fprintln(out, s.wordmark(center(row, width)))
	}
	fmt.Fprintln(out, s.muted(center(theme.Tagline, width)))
	fmt.Fprintln(out)
	fmt.Fprintln(out, fit("  "+theme.Pitch, width))
	fmt.Fprintln(out, s.muted(fit("  "+theme.Disclaimer, width)))
	fmt.Fprintln(out)
	metadata := "  " + model.Version + " · build " + model.Build
	fmt.Fprintln(out, s.muted(fit(metadata, width)))
	statusLine(out, width, s, "Pipeline", "connected locally; static analysis only")
	statusLine(out, width, s, "Repository", model.Status.Repository+" ["+model.Status.Branch+"]")
	statusLine(out, width, s, "Policy", model.Status.FailMode+" · PyPI + npm registries")
	fmt.Fprintln(out)

	boxTop(out, width, s)
	boxContent(out, width, s, "GET STARTED")
	boxContent(out, width, s, "  scan staged     inspect exactly what is staged for commit")
	boxContent(out, width, s, "  scan all        inspect supported files in this repository")
	boxContent(out, width, s, "  status          show repository and policy state")
	boxContent(out, width, s, "  help            list terminal workspace commands")
	boxBottom(out, width, s)
	fmt.Fprintln(out)
	cta := "Type scan staged to inspect the next commit. Nothing is changed by this workspace."
	fmt.Fprintln(out, s.accentText(fit("  "+cta, width)))
}

func renderCompactWelcome(out io.Writer, width int, s style, theme Theme, model WelcomeModel) {
	boxTop(out, width, s)
	boxContent(out, width, s, theme.Name+" — "+model.Version)
	boxContent(out, width, s, theme.Tagline)
	boxContent(out, width, s, "repo: "+model.Status.Repository+" ["+model.Status.Branch+"]")
	boxContent(out, width, s, "policy: "+model.Status.FailMode+" · scanner ready")
	boxBottom(out, width, s)
	fmt.Fprintln(out)
	fmt.Fprintln(out, s.muted(fit("  "+theme.Pitch, width)))
	fmt.Fprintln(out, s.accentText(fit("  Start: scan staged | help", width)))
}

func boxTop(out io.Writer, width int, s style) {
	fmt.Fprintln(out, s.accentText("  ╭"+strings.Repeat("─", width-4)+"╮"))
}

func boxBottom(out io.Writer, width int, s style) {
	fmt.Fprintln(out, s.accentText("  ╰"+strings.Repeat("─", width-4)+"╯"))
}

func boxContent(out io.Writer, width int, s style, content string) {
	content = fit(content, width-5)
	padding := strings.Repeat(" ", width-5-runeWidth(content))
	fmt.Fprintln(out, s.accentText("  │")+" "+content+padding+s.accentText("│"))
}

func statusLine(out io.Writer, width int, s style, label, value string) {
	left := s.accentText("  ●") + " " + label + ": "
	available := width - 4 - runeWidth(label)
	fmt.Fprintln(out, left+s.muted(fit(value, available)))
}

func wordmarkRows(colour bool) []string {
	const on = "█"
	const off = " "
	glyphs := map[rune][]string{
		'P': {on + on + on + off, on + off + off + on, on + on + on + off, on + off + off + off, on + off + off + off},
		'H': {on + off + off + on, on + off + off + on, on + on + on + on, on + off + off + on, on + off + off + on},
		'A': {off + on + on + off, on + off + off + on, on + on + on + on, on + off + off + on, on + off + off + on},
		'N': {on + off + off + on, on + on + off + on, on + off + on + on, on + off + off + on, on + off + off + on},
		'T': {on + on + on + on, off + on + on + off, off + on + on + off, off + on + on + off, off + on + on + off},
		'O': {off + on + on + off, on + off + off + on, on + off + off + on, on + off + off + on, off + on + on + off},
		'M': {on + off + off + on, on + on + on + on, on + on + on + on, on + off + off + on, on + off + off + on},
		'G': {off + on + on + on, on + off + off + off, on + off + on + on, on + off + off + on, off + on + on + on},
		'U': {on + off + off + on, on + off + off + on, on + off + off + on, on + off + off + on, off + on + on + off},
		'R': {on + on + on + off, on + off + off + on, on + on + on + off, on + off + on + off, on + off + off + on},
		'D': {on + on + on + off, on + off + off + on, on + off + off + on, on + off + off + on, on + on + on + off},
	}
	letters := "PHANTOM GUARD"
	rows := make([]string, 5)
	for row := range rows {
		var line strings.Builder
		for _, letter := range letters {
			if letter == ' ' {
				line.WriteString("  ")
				continue
			}
			line.WriteString(glyphs[letter][row])
			line.WriteByte(' ')
		}
		// A dim, offset glyph is drawn inside each filled block by using the
		// shaded right edge. It gives the wordmark its terminal-native layering.
		rows[row] = strings.TrimRight(strings.ReplaceAll(line.String(), "█ ", "█░"), " ")
		if !colour {
			rows[row] = strings.NewReplacer("█", "#", "░", " ").Replace(rows[row])
		}
	}
	return rows
}

func usableWidth(width int) int {
	if width < minimumWidth {
		return minimumWidth
	}
	return width
}

func terminalWidth() int {
	if value, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && value > 0 {
		return value
	}
	return defaultWidth
}

func fit(value string, width int) string {
	if width <= 1 {
		return ""
	}
	if runeWidth(value) <= width {
		return value
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}

func center(value string, width int) string {
	value = fit(value, width)
	return strings.Repeat(" ", max(0, (width-runeWidth(value))/2)) + value
}

func runeWidth(value string) int { return utf8.RuneCountInString(value) }

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
