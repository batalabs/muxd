package tui

import "github.com/charmbracelet/lipgloss"

var (
	WelcomeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	UserIconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	AsstIconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	PromptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("183"))
	InputStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	CursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ThinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	FooterHead   = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	FooterTokens = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	FooterMeta   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	ErrorLineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	BulletStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("180"))
	HeadingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Bold(true)
	CodeGutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	ToolNameStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("147")).Bold(true)
	ToolInputStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	ToolResultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))

	BoldInlineStyle    = lipgloss.NewStyle().Bold(true)
	ItalicInlineStyle  = lipgloss.NewStyle().Italic(true)
	StrikethroughStyle = lipgloss.NewStyle().Strikethrough(true)
	LinkTextStyle      = lipgloss.NewStyle()
	LinkURLStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	InlineCodeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	HrStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	BlockquoteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	TableBorderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	TableHeaderStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))

	CompletionStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	CompletionSelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("62"))
)
