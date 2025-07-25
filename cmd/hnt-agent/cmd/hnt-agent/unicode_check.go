package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/veilm/hinata/cmd/hnt-agent/pkg/spinner"
)

func runUnicodeCheck(cmd *cobra.Command, args []string) {
	fmt.Println("=== UNICODE DETECTION SYSTEM TEST ===\n")

	// 1. Environment Variables
	fmt.Println("1. ENVIRONMENT VARIABLES:")
	fmt.Println("-------------------------")
	envVars := []string{"HINATA_ENABLE_UNICODE_DETECTION", "NO_UNICODE", "TERM", "COLORTERM", "LANG", "LC_ALL", "LC_CTYPE", "SSH_CLIENT", "SSH_TTY", "SSH_CONNECTION"}
	for _, v := range envVars {
		val := os.Getenv(v)
		if val == "" {
			fmt.Printf("%-35s: (not set)\n", v)
		} else {
			fmt.Printf("%-35s: %s\n", v, val)
		}
	}

	// 2. SSH Session Detection
	fmt.Println("\n2. SSH SESSION DETECTION:")
	fmt.Println("-------------------------")
	sshClient := os.Getenv("SSH_CLIENT")
	sshTTY := os.Getenv("SSH_TTY")
	sshConnection := os.Getenv("SSH_CONNECTION")
	isSSH := sshClient != "" || sshTTY != "" || sshConnection != ""
	fmt.Printf("SSH_CLIENT:     %v\n", sshClient != "")
	fmt.Printf("SSH_TTY:        %v\n", sshTTY != "")
	fmt.Printf("SSH_CONNECTION: %v\n", sshConnection != "")
	fmt.Printf("In SSH session: %v\n", isSSH)
	if isSSH {
		fmt.Println("NOTE: Unicode support capped at Basic in SSH sessions")
	}

	// 3. Locale Detection
	fmt.Println("\n3. LOCALE DETECTION:")
	fmt.Println("--------------------")
	localeVars := []string{"LC_ALL", "LC_CTYPE", "LANG"}
	utf8Found := false
	for _, v := range localeVars {
		if locale := os.Getenv(v); locale != "" {
			isUTF8 := strings.Contains(strings.ToLower(locale), "utf-8") ||
				strings.Contains(strings.ToLower(locale), "utf8")
			fmt.Printf("%-12s: %s (UTF-8: %v)\n", v, locale, isUTF8)
			if isUTF8 {
				utf8Found = true
			}
		}
	}
	fmt.Printf("UTF-8 locale detected: %v\n", utf8Found)

	// 4. Terminal Detection
	fmt.Println("\n4. TERMINAL DETECTION:")
	fmt.Println("----------------------")
	term := strings.ToLower(os.Getenv("TERM"))
	fmt.Printf("TERM value: %s\n", os.Getenv("TERM"))
	fmt.Printf("Is Linux console: %v\n",
		strings.Contains(term, "linux") && !strings.Contains(term, "xterm"))

	// Check modern terminal list
	modernTerms := []string{
		"xterm-256color", "screen-256color", "tmux-256color",
		"rxvt-unicode", "alacritty", "kitty", "wezterm",
		"foot", "gnome-256color", "konsole",
	}
	isModern := false
	for _, mt := range modernTerms {
		if strings.Contains(term, mt) {
			isModern = true
			fmt.Printf("Matches modern terminal: %s\n", mt)
			break
		}
	}
	if !isModern {
		fmt.Println("No modern terminal match")
	}

	// 5. Font Detection
	fmt.Println("\n5. FONT DETECTION (fc-list):")
	fmt.Println("----------------------------")

	// Check if fc-list exists
	if _, err := exec.LookPath("fc-list"); err != nil {
		fmt.Println("fc-list: NOT AVAILABLE")
	} else {
		fmt.Println("fc-list: Available")

		// Phase 1: Check for U+1FB90
		fmt.Println("\nPhase 1 - Querying fonts with U+1FB90:")
		cmd := exec.Command("fc-list", ":charset=1fb90")
		output, err := cmd.Output()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fonts := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(fonts) == 1 && fonts[0] == "" {
				fmt.Println("No fonts found with U+1FB90 support")
			} else {
				// Filter out LastResort fonts
				filteredFonts := []string{}
				for _, font := range fonts {
					if font != "" && !strings.Contains(font, "LastResort") {
						filteredFonts = append(filteredFonts, font)
					}
				}

				fmt.Printf("Found %d font(s) (excluding LastResort):\n", len(filteredFonts))
				for i, font := range filteredFonts {
					fmt.Printf("  [%d] %s\n", i+1, font)
				}

				// Check for known good fonts
				fmt.Println("\nChecking for known good fonts:")
				goodFonts := []string{
					"cascadia code", "cascadia mono", "gnu unifont", "unifont",
					"fairfax hd", "fairfax", "legacy_computing", "unscii", "adwaita mono",
				}
				foundGood := false
				for _, font := range filteredFonts {
					fontLower := strings.ToLower(font)
					for _, gf := range goodFonts {
						if strings.Contains(fontLower, gf) {
							fmt.Printf("  ✓ Found known good font: %s\n", gf)
							foundGood = true
							break
						}
					}
				}
				if !foundGood {
					fmt.Println("  ✗ No known good fonts found")
				}
			}
		}

		// Phase 2: Multiple character testing
		fmt.Println("\nPhase 2 - Testing multiple Legacy Computing characters:")
		testChars := []string{"1fb90", "1fb95", "1fba0", "1fbb0"}
		supportCount := 0

		for _, char := range testChars {
			cmd := exec.Command("fc-list", ":charset="+char, "family", "style")
			output, err := cmd.Output()
			if err == nil && len(bytes.TrimSpace(output)) > 0 {
				families := strings.Split(strings.TrimSpace(string(output)), "\n")
				// Filter out LastResort fonts
				nonLastResortCount := 0
				for _, family := range families {
					if family != "" && !strings.Contains(family, "LastResort") {
						nonLastResortCount++
					}
				}

				if nonLastResortCount > 0 {
					supportCount++
					fmt.Printf("  ✓ U+%s supported (%d fonts, excluding LastResort)\n",
						strings.ToUpper(char), nonLastResortCount)
				} else {
					fmt.Printf("  ✗ U+%s not supported (only LastResort found)\n", strings.ToUpper(char))
				}
			} else {
				fmt.Printf("  ✗ U+%s not supported\n", strings.ToUpper(char))
			}
		}
		fmt.Printf("\nSupport count: %d/4 (threshold: 3)\n", supportCount)
		fmt.Printf("Multiple character test: %v\n", supportCount >= 3)
	}

	// 6. Detection Flow
	fmt.Println("\n6. DETECTION FLOW:")
	fmt.Println("------------------")

	// Show step-by-step what happened
	if os.Getenv("HINATA_ENABLE_UNICODE_DETECTION") == "" {
		fmt.Println("Step 0: HINATA_ENABLE_UNICODE_DETECTION not set → SKIP DETECTION (default: Full Unicode)")
	} else if os.Getenv("NO_UNICODE") != "" {
		fmt.Println("Step 1: NO_UNICODE is set → STOP (ASCII only)")
	} else {
		fmt.Println("Step 1: NO_UNICODE not set → continue")

		if !utf8Found {
			fmt.Println("Step 2: No UTF-8 locale → STOP (ASCII only)")
		} else {
			fmt.Println("Step 2: UTF-8 locale found → continue")

			if isSSH {
				fmt.Println("Step 3: SSH session detected → STOP (Basic Unicode)")
			} else {
				fmt.Println("Step 3: Not in SSH session → continue")

				if strings.Contains(term, "linux") && !strings.Contains(term, "xterm") {
					fmt.Println("Step 4: Linux console detected → STOP (Basic Unicode)")
				} else {
					fmt.Println("Step 4: Not Linux console → continue")
					fmt.Println("Step 5: Check font support → determines Full vs Basic Unicode")
				}
			}
		}
	}

	// 7. Final Detection Result
	fmt.Println("\n7. FINAL DETECTION RESULT:")
	fmt.Println("--------------------------")
	support := spinner.GetUnicodeSupport()
	fmt.Printf("Detected Unicode Support Level: %s\n", spinner.GetUnicodeSupportString())
	fmt.Printf("Raw value: %d (0=None, 1=Basic, 2=Full)\n", support)

	// 8. Spinner Filtering
	fmt.Println("\n8. SPINNER FILTERING:")
	fmt.Println("--------------------")
	fmt.Printf("Total spinners available: %d\n", len(spinner.SPINNERS))

	// Count complex Unicode spinners
	complexCount := 0
	exampleComplex := ""
	for _, s := range spinner.SPINNERS {
		hasComplex := false
		for _, frame := range s.Frames {
			for _, r := range frame {
				if spinner.IsComplexUnicodeChar(r) {
					hasComplex = true
					if exampleComplex == "" {
						exampleComplex = fmt.Sprintf("%s (contains U+%04X)", s.Name, r)
					}
					break
				}
			}
			if hasComplex {
				break
			}
		}
		if hasComplex {
			complexCount++
		}
	}
	fmt.Printf("Spinners with complex Unicode: %d\n", complexCount)
	if exampleComplex != "" {
		fmt.Printf("Example: %s\n", exampleComplex)
	}

	// 9. Test Scenarios
	fmt.Println("\n9. TEST OTHER SCENARIOS:")
	fmt.Println("------------------------")
	fmt.Println("To test different scenarios, run with environment variables:")
	fmt.Println("  NO_UNICODE=1 hnt-agent unicode-check    # Force ASCII")
	fmt.Println("  TERM=linux hnt-agent unicode-check      # Linux console")
	fmt.Println("  LC_ALL=C LANG=C hnt-agent unicode-check # Non-UTF8 locale")
	fmt.Println("  TERM=xterm-mono hnt-agent unicode-check # Unknown terminal")
}
