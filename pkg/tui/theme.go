// Package tui provides PhantomGuard's interactive terminal workspace.
package tui

// Theme holds the small set of visual decisions for the terminal workspace.
// Keeping copy and colour here makes the splash easy to rebrand without
// touching its layout or scanner integration.
type Theme struct {
	Name          string
	Tagline       string
	Pitch         string
	Disclaimer    string
	AccentRGB     [3]uint8
	Prompt        string
	PromptHint    string
	WelcomePrefix string
}

// DefaultTheme uses a single burgundy red accent (#9f1239) for a focused,
// high-contrast terminal identity. The terminal interface uses no other
// foreground colour.
var DefaultTheme = Theme{
	Name:          "PhantomGuard",
	Tagline:       "Stop phantom dependencies before they ship.",
	Pitch:         "Verify the dependencies your code imports before they become a supply-chain risk.",
	Disclaimer:    "Registry results are evidence, not a guarantee—review every change before committing.",
	AccentRGB:     [3]uint8{159, 18, 57},
	Prompt:        "pg›",
	PromptHint:    "scan staged, scan all, status, help, quit",
	WelcomePrefix: "Welcome to",
}
