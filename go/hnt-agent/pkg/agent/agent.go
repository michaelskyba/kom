package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hnt-agent/pkg/spinner"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"shell-exec/pkg/shell"
	"strings"
	"time"

	"github.com/fatih/color"
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
	
	shellExecutor   *shell.Executor
	turnCounter     int
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
		ConversationDir: cfg.ConversationDir,
		SystemPrompt:    cfg.SystemPrompt,
		Model:           cfg.Model,
		IgnoreReasoning: cfg.IgnoreReasoning,
		NoConfirm:       cfg.NoConfirm,
		NoEscape:        cfg.NoEscape,
		ShellDisplay:    cfg.ShellDisplay,
		UseJSON:         cfg.UseJSON,
		SpinnerIndex:    cfg.SpinnerIndex,
		shellExecutor:   executor,
		turnCounter:     1,
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
		
		configDir, _ := os.UserConfigDir()
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
	fmt.Println("\n")
	
	taggedMessage := fmt.Sprintf("<user_request>\n%s\n</user_request>", userMessage)
	if err := a.writeMessage("user", taggedMessage); err != nil {
		return err
	}
	
	for {
		llmResponse, err := a.generateLLMResponse()
		if err != nil {
			return fmt.Errorf("LLM error: %w", err)
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
				fmt.Println("\n")
				
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
				if !a.promptExecute() {
					newMessage := a.promptForMessage()
					if newMessage == "" {
						return fmt.Errorf("no message provided")
					}
					
					a.printTurnHeader("querent", a.humanTurnCounter)
					a.humanTurnCounter++
					fmt.Print(indentMultiline(newMessage))
					fmt.Println("\n")
					
					taggedMessage := fmt.Sprintf("<user_request>\n%s\n</user_request>", newMessage)
					if err := a.writeMessage("user", taggedMessage); err != nil {
						return err
					}
					continue
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
	packCmd := exec.Command("hnt-chat", "pack", a.ConversationDir)
	packedConv, err := packCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to pack conversation: %w", err)
	}
	
	llmCmd := exec.Command("hnt-llm", "--model", a.Model)
	if !a.IgnoreReasoning {
		llmCmd.Args = append(llmCmd.Args, "--reasoning")
	}
	
	llmCmd.Stdin = strings.NewReader(string(packedConv))
	output, err := llmCmd.Output()
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	
	return string(output), nil
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
	cmd := exec.Command("hnt-chat", "add", a.ConversationDir, role)
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
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
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s[y/n]: ", marginStr())
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

func (a *Agent) promptExecute() bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s[y/n]: ", marginStr())
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

func (a *Agent) promptForMessage() string {
	fmt.Printf("%sEnter your message (end with Ctrl+D):\n", marginStr())
	reader := bufio.NewReader(os.Stdin)
	var lines []string
	
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		lines = append(lines, line)
	}
	
	return strings.TrimSpace(strings.Join(lines, ""))
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
		fmt.Println("\n")
	}
}

func getTerminalWidth() int {
	cmd := exec.Command("tput", "cols")
	output, err := cmd.Output()
	if err != nil {
		return 80
	}
	
	var width int
	fmt.Sscanf(string(output), "%d", &width)
	return width
}