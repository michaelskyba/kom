package spinner

import (
	"fmt"
	"math/rand"
	"time"
)

type Spinner struct {
	Name   string
	Frames []string
	Speed  time.Duration
}

var SPINNERS = []Spinner{
	{
		Name:   "dots",
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		Speed:  80 * time.Millisecond,
	},
	{
		Name:   "dots2",
		Frames: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"},
		Speed:  80 * time.Millisecond,
	},
	{
		Name:   "dots3",
		Frames: []string{"⠋", "⠙", "⠚", "⠞", "⠖", "⠦", "⠴", "⠲", "⠳", "⠓"},
		Speed:  80 * time.Millisecond,
	},
	{
		Name:   "line",
		Frames: []string{"-", "\\", "|", "/"},
		Speed:  100 * time.Millisecond,
	},
	{
		Name:   "pipe",
		Frames: []string{"┤", "┘", "┴", "└", "├", "┌", "┬", "┐"},
		Speed:  100 * time.Millisecond,
	},
	{
		Name:   "star",
		Frames: []string{"✶", "✸", "✹", "✺", "✹", "✸"},
		Speed:  70 * time.Millisecond,
	},
	{
		Name:   "toggle",
		Frames: []string{"⊶", "⊷"},
		Speed:  250 * time.Millisecond,
	},
	{
		Name:   "arrow",
		Frames: []string{"←", "↖", "↑", "↗", "→", "↘", "↓", "↙"},
		Speed:  100 * time.Millisecond,
	},
	{
		Name:   "bouncing",
		Frames: []string{"⠁", "⠂", "⠄", "⠂"},
		Speed:  120 * time.Millisecond,
	},
	{
		Name:   "bouncing_bar",
		Frames: []string{"[    ]", "[=   ]", "[==  ]", "[=== ]", "[ ===]", "[  ==]", "[   =]", "[    ]", "[   =]", "[  ==]", "[ ===]", "[====]", "[=== ]", "[==  ]", "[=   ]"},
		Speed:  80 * time.Millisecond,
	},
	{
		Name:   "moon",
		Frames: []string{"🌑", "🌒", "🌓", "🌔", "🌕", "🌖", "🌗", "🌘"},
		Speed:  200 * time.Millisecond,
	},
	{
		Name:   "hearts",
		Frames: []string{"💛", "💙", "💜", "💚", "❤️"},
		Speed:  100 * time.Millisecond,
	},
	{
		Name:   "clock",
		Frames: []string{"🕐", "🕑", "🕒", "🕓", "🕔", "🕕", "🕖", "🕗", "🕘", "🕙", "🕚", "🕛"},
		Speed:  100 * time.Millisecond,
	},
	{
		Name:   "earth",
		Frames: []string{"🌍", "🌎", "🌏"},
		Speed:  180 * time.Millisecond,
	},
}

var loadingMessages = []string{
	"Pondering the query",
	"Processing request",
	"Consulting the shell",
	"Working on it",
	"Computing response",
	"Analyzing input",
	"Thinking deeply",
	"Formulating answer",
	"Executing logic",
	"Preparing output",
}

func GetRandomSpinner() Spinner {
	return SPINNERS[rand.Intn(len(SPINNERS))]
}

func GetRandomLoadingMessage() string {
	return loadingMessages[rand.Intn(len(loadingMessages))]
}

func Run(spinner Spinner, message string, margin string, stopCh <-chan bool) {
	fmt.Printf("%s%s", margin, message)

	frameIndex := 0
	ticker := time.NewTicker(spinner.Speed)
	defer ticker.Stop()

	hideCursor()
	defer showCursor()

	for {
		select {
		case <-stopCh:
			clearLine()
			return
		case <-ticker.C:
			frame := spinner.Frames[frameIndex]
			fmt.Printf("\r%s%s %s", margin, frame, message)
			frameIndex = (frameIndex + 1) % len(spinner.Frames)
		}
	}
}

func hideCursor() {
	fmt.Print("\033[?25l")
}

func showCursor() {
	fmt.Print("\033[?25h")
}

func clearLine() {
	fmt.Print("\r\033[K")
}
