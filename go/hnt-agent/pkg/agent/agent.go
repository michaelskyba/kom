package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hnt-agent/pkg/spinner"
	"os"
	"path/filepath"
	"regexp"
	"shared/pkg/prompt"
	"shell-exec/pkg/shell"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fatih/color"
	"github.com/veilm/hinata/hnt-chat/pkg/chat"
	"github.com/veilm/hinata/hnt-llm/pkg/llm"
	"github.com/veilm/hinata/tui-select/pkg/selector"
	"golang.org/x/term"
)

const MARGIN = 2

type Agent struct {
	ConversationDir string
	SystemPrompt    string
	Model           string
	IgnoreReasoning bool
	NoConfirm       bool
	NoEscape        bool
	ShellDisplay    bool
	UseJSON         bool
	SpinnerIndex    *int
	UseEditor       bool

	shellExecutor    *shell.Executor
	turnCounter      int
	humanTurnCounter int
}

type Config struct {
	ConversationDir string
	SystemPrompt    string
	Model           string
	PWD             string
	IgnoreReasoning bool
	NoConfirm       bool
	NoEscape        bool
	ShellDisplay    bool
	UseJSON         bool
	SpinnerIndex    *int
	UseEditor       bool
}

func New(cfg Config) (*Agent, error) {
	if cfg.ConversationDir == "" {
		tmpDir, err := os.MkdirTemp("", "hnt-agent-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
		cfg.ConversationDir = tmpDir
	}

	pwd := cfg.PWD
	if pwd == "" {
		var err error
		pwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	if cfg.Model == "" {
		cfg.Model = os.Getenv("HINATA_AGENT_MODEL")
		if cfg.Model == "" {
			cfg.Model = os.Getenv("HINATA_MODEL")
			if cfg.Model == "" {
				cfg.Model = "openrouter/google/gemini-2.5-pro"
			}
		}
	}

	executor := shell.NewExecutor(pwd)

	if existingPwd, err := os.ReadFile(filepath.Join(cfg.ConversationDir, "hnt-agent-pwd.txt")); err == nil {
		executor.WorkingDir = strings.TrimSpace(string(existingPwd))
	}

	if existingEnv, err := os.ReadFile(filepath.Join(cfg.ConversationDir, "hnt-agent-env.json")); err == nil {
		var env map[string]string
		if err := json.Unmarshal(existingEnv, &env); err == nil {
			executor.Env = env
		}
	}

	return &Agent{
		ConversationDir:  cfg.ConversationDir,
		SystemPrompt:     cfg.SystemPrompt,
		Model:            cfg.Model,
		IgnoreReasoning:  cfg.IgnoreReasoning,
		NoConfirm:        cfg.NoConfirm,
		NoEscape:         cfg.NoEscape,
		ShellDisplay:     cfg.ShellDisplay,
		UseJSON:          cfg.UseJSON,
		SpinnerIndex:     cfg.SpinnerIndex,
		UseEditor:        cfg.UseEditor,
		shellExecutor:    executor,
		turnCounter:      1,
		humanTurnCounter: 1,
	}, nil
}

func (a *Agent) Run(userMessage string) error {
	isNewSession := !a.isExistingSession()

	if isNewSession {
		if a.SystemPrompt != "" {
			if err := a.writeMessage("system", a.SystemPrompt); err != nil {
				return err
			}
		}

		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir, _ = os.UserConfigDir()
		}
		hinataMdPath := filepath.Join(configDir, "hinata/agent/HINATA.md")
		if content, err := os.ReadFile(hinataMdPath); err == nil && len(content) > 0 {
			message := fmt.Sprintf("<info>\n%s\n</info>", string(content))
			if err := a.writeMessage("user", message); err != nil {
				return err
			}
		}
	} else {
		a.resumeSession()
	}

	a.printTurnHeader("querent", a.humanTurnCounter)
	a.humanTurnCounter++
	fmt.Print(indentMultiline(userMessage))
	fmt.Println()

	taggedMessage := fmt.Sprintf("<user_request>\n%s\n</user_request>", userMessage)
	if err := a.writeMessage("user", taggedMessage); err != nil {
		return err
	}

	for {
		llmResponse, err := a.generateLLMResponse()
		if err != nil {
			return fmt.Errorf("failed to generate LLM response: %w", err)
		}

		a.printTurnHeader("hinata", a.turnCounter)
		a.turnCounter++

		fmt.Print(indentMultiline(llmResponse))
		fmt.Println()

		if err := a.writeMessage("assistant", llmResponse); err != nil {
			return err
		}

		shellCommands := extractShellCommands(llmResponse)
		if len(shellCommands) == 0 {
			if !a.NoConfirm {
				fmt.Printf("\n%sHinata has completed its response. Continue?\n", marginStr())
				if !a.promptContinue() {
					return nil
				}

				newMessage := a.promptForMessage()
				if newMessage == "" {
					return fmt.Errorf("no message provided")
				}

				a.printTurnHeader("querent", a.humanTurnCounter)
				a.humanTurnCounter++
				fmt.Print(indentMultiline(newMessage))
				fmt.Println()

				taggedMessage := fmt.Sprintf("<user_request>\n%s\n</user_request>", newMessage)
				if err := a.writeMessage("user", taggedMessage); err != nil {
					return err
				}
			} else {
				return nil
			}
		} else {
			commands := shellCommands[len(shellCommands)-1]

			if !a.NoEscape {
				re := regexp.MustCompile(`(^|[^\\])` + "`")
				commands = re.ReplaceAllString(commands, "$1\\`")
			}

			if !a.NoConfirm {
				fmt.Printf("\n%sHinata wants to execute a shell block. Proceed?\n", marginStr())
				choice := a.promptExecute()
				switch choice {
				case executeExit:
					return nil
				case executeSkip:
					newMessage := a.promptForMessage()
					if newMessage == "" {
						return fmt.Errorf("no message provided")
					}

					a.printTurnHeader("querent", a.humanTurnCounter)
					a.humanTurnCounter++
					fmt.Print(indentMultiline(newMessage))
					fmt.Println()

					taggedMessage := fmt.Sprintf("<user_request>\n%s\n</user_request>", newMessage)
					if err := a.writeMessage("user", taggedMessage); err != nil {
						return err
					}
					continue
				case executeYes:
					// Continue with execution
				}
			}

			result, err := a.executeShellCommands(commands)
			if err != nil {
				return fmt.Errorf("shell execution error: %w", err)
			}

			if err := a.saveState(); err != nil {
				return fmt.Errorf("failed to save state: %w", err)
			}

			var resultMessage string
			if a.UseJSON {
				jsonResult := map[string]interface{}{
					"stdout":    result.Stdout,
					"stderr":    result.Stderr,
					"exit_code": result.ExitCode,
				}
				jsonBytes, _ := json.MarshalIndent(jsonResult, "", "  ")
				resultMessage = fmt.Sprintf("<hnt-shell-results>\n%s\n</hnt-shell-results>", string(jsonBytes))
			} else {
				resultMessage = formatShellResults(result)
			}

			if err := a.writeMessage("user", resultMessage); err != nil {
				return err
			}

			if a.ShellDisplay {
				fmt.Println()
				fmt.Print(indentMultiline(resultMessage))
				fmt.Println()
			}
		}
	}
}

func (a *Agent) generateLLMResponse() (string, error) {
	var packedBuf bytes.Buffer
	err := chat.PackConversation(a.ConversationDir, &packedBuf, false)
	if err != nil {
		return "", fmt.Errorf("failed to pack conversation: %w", err)
	}

	config := llm.Config{
		Model:            a.Model,
		IncludeReasoning: !a.IgnoreReasoning,
	}

	ctx := context.Background()
	eventChan, errChan := llm.StreamLLMResponse(ctx, config, packedBuf.String())

	var response strings.Builder
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				return response.String(), nil
			}
			response.WriteString(event.Content)
			if event.Reasoning != "" && !a.IgnoreReasoning {
				response.WriteString(event.Reasoning)
			}
		case err := <-errChan:
			if err != nil {
				return "", fmt.Errorf("LLM request failed: %w\nModel: %s", err, a.Model)
			}
		}
	}
}

func (a *Agent) executeShellCommands(commands string) (*shell.ExecutionResult, error) {
	stopCh := make(chan bool)

	sp := spinner.GetRandomSpinner()
	if a.SpinnerIndex != nil && *a.SpinnerIndex < len(spinner.SPINNERS) {
		sp = spinner.SPINNERS[*a.SpinnerIndex]
	}

	msg := spinner.GetRandomLoadingMessage()

	go spinner.Run(sp, msg, marginStr(), stopCh)

	result, err := a.shellExecutor.Execute(commands)

	close(stopCh)
	time.Sleep(50 * time.Millisecond)

	return result, err
}

func (a *Agent) writeMessage(role, content string) error {
	chatRole, err := chat.ParseRole(role)
	if err != nil {
		return fmt.Errorf("invalid role %s: %w", role, err)
	}

	_, err = chat.WriteMessageFile(a.ConversationDir, chatRole, content)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	return nil
}

func (a *Agent) saveState() error {
	pwdFile := filepath.Join(a.ConversationDir, "hnt-agent-pwd.txt")
	if err := os.WriteFile(pwdFile, []byte(a.shellExecutor.WorkingDir), 0644); err != nil {
		return err
	}

	envFile := filepath.Join(a.ConversationDir, "hnt-agent-env.json")
	envData, err := json.MarshalIndent(a.shellExecutor.Env, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(envFile, envData, 0644)
}

func (a *Agent) printTurnHeader(role string, turn int) {
	width := getTerminalWidth()

	var icon string
	var lineColor *color.Color

	switch role {
	case "hinata":
		icon = "❄️"
		lineColor = color.New(color.FgBlue)
	case "querent":
		icon = "🗝️"
		lineColor = color.New(color.FgMagenta)
	default:
		icon = "?"
		lineColor = color.New(color.FgWhite)
	}

	roleText := fmt.Sprintf("%s %s", icon, role)
	turnText := fmt.Sprintf("turn %d", turn)
	prefix := "─────── "

	totalLen := len(prefix) + len(roleText) + len(" • ") + len(turnText) + 1
	lineLen := width - totalLen - MARGIN*2
	if lineLen < 0 {
		lineLen = 0
	}

	line := strings.Repeat("─", lineLen)

	fmt.Print(marginStr())
	lineColor.Print(prefix)
	fmt.Print(roleText)
	lineColor.Print(" • ")
	green := color.New(color.FgGreen)
	green.Print(turnText)
	fmt.Print(" ")
	lineColor.Print(line)
	fmt.Println()
}

func (a *Agent) promptContinue() bool {
	items := []string{"Continue conversation", "Exit"}
	opts := selector.Options{
		Height: 2,
		Color:  4, // Blue
		Prefix: "→ ",
	}

	model := selector.New(items, opts)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return false
	}

	final := finalModel.(selector.Model)
	if final.Aborted() {
		return false
	}

	return final.Choice() == "Continue conversation"
}

type executeChoice int

const (
	executeYes executeChoice = iota
	executeSkip
	executeExit
)

func (a *Agent) promptExecute() executeChoice {
	items := []string{"Execute shell commands", "Skip and continue conversation", "Exit"}
	opts := selector.Options{
		Height: 3,
		Color:  3, // Yellow
		Prefix: "→ ",
	}

	model := selector.New(items, opts)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return executeExit
	}

	final := finalModel.(selector.Model)
	if final.Aborted() {
		return executeExit
	}

	switch final.Choice() {
	case "Execute shell commands":
		return executeYes
	case "Skip and continue conversation":
		return executeSkip
	default:
		return executeExit
	}
}

func (a *Agent) promptForMessage() string {
	fmt.Printf("\n%sPlease provide new instructions:\n", marginStr())

	instruction, err := prompt.GetUserInstruction("", a.UseEditor)
	if err != nil {
		if a.UseEditor {
			fmt.Fprintf(os.Stderr, "%sError: %v\n", marginStr(), err)
		}
		return ""
	}

	return instruction
}

func extractShellCommands(text string) []string {
	re := regexp.MustCompile(`(?s)<hnt-shell>(.*?)</hnt-shell>`)
	matches := re.FindAllStringSubmatch(text, -1)

	var commands []string
	for _, match := range matches {
		if len(match) > 1 {
			commands = append(commands, strings.TrimSpace(match[1]))
		}
	}

	return commands
}

func formatShellResults(result *shell.ExecutionResult) string {
	var parts []string
	parts = append(parts, "<hnt-shell-results>")

	if result.Stdout != "" {
		parts = append(parts, "<stdout>")
		parts = append(parts, result.Stdout)
		parts = append(parts, "</stdout>")
	}

	if result.Stderr != "" {
		parts = append(parts, "<stderr>")
		parts = append(parts, result.Stderr)
		parts = append(parts, "</stderr>")
	}

	parts = append(parts, fmt.Sprintf("<exit-code>%d</exit-code>", result.ExitCode))
	parts = append(parts, "</hnt-shell-results>")

	return strings.Join(parts, "\n")
}

func marginStr() string {
	return strings.Repeat(" ", MARGIN)
}

func indentMultiline(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = marginStr() + lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

func (a *Agent) isExistingSession() bool {
	entries, err := os.ReadDir(a.ConversationDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "-system.md") ||
			strings.HasSuffix(entry.Name(), "-user.md") ||
			strings.HasSuffix(entry.Name(), "-assistant.md") {
			return true
		}
	}

	return false
}

func (a *Agent) resumeSession() {
	entries, _ := os.ReadDir(a.ConversationDir)

	var assistantCount, userCount int
	var lastAssistantMessage string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, "-assistant.md") {
			assistantCount++
			content, _ := os.ReadFile(filepath.Join(a.ConversationDir, name))
			lastAssistantMessage = string(content)
		} else if strings.HasSuffix(name, "-user.md") {
			content, _ := os.ReadFile(filepath.Join(a.ConversationDir, name))
			if strings.Contains(string(content), "<user_request>") {
				userCount++
			}
		}
	}

	if lastAssistantMessage != "" {
		a.humanTurnCounter = userCount + 1
		a.turnCounter = assistantCount + 1

		a.printTurnHeader("hinata", assistantCount)
		fmt.Print(indentMultiline(lastAssistantMessage))
		fmt.Println()
	}
}

func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80
	}
	return width
}
