package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// readClipboard retrieves the current plain text from the OS clipboard.
func readClipboard() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbpaste")
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("pbpaste failed: %w", err)
		}
		return string(out), nil
	case "linux":
		if path, err := exec.LookPath("wl-paste"); err == nil {
			cmd := exec.Command(path)
			if out, err := cmd.Output(); err == nil {
				return string(out), nil
			}
		}
		if path, err := exec.LookPath("xclip"); err == nil {
			cmd := exec.Command(path, "-selection", "clipboard", "-o")
			if out, err := cmd.Output(); err == nil {
				return string(out), nil
			}
		}
		if path, err := exec.LookPath("xsel"); err == nil {
			cmd := exec.Command(path, "--clipboard", "--output")
			if out, err := cmd.Output(); err == nil {
				return string(out), nil
			}
		}
		return "", fmt.Errorf("no clipboard utility found (install wl-paste, xclip, or xsel)")
	case "windows":
		cmd := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard")
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("powershell Get-Clipboard failed: %w", err)
		}
		return strings.ReplaceAll(string(out), "\r\n", "\n"), nil
	default:
		return "", fmt.Errorf("unsupported OS for clipboard: %s", runtime.GOOS)
	}
}

// writeClipboard copies the provided text into the OS clipboard.
func writeClipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	case "linux":
		if path, err := exec.LookPath("wl-copy"); err == nil {
			cmd := exec.Command(path)
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		if path, err := exec.LookPath("xclip"); err == nil {
			cmd := exec.Command(path, "-selection", "clipboard")
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		if path, err := exec.LookPath("xsel"); err == nil {
			cmd := exec.Command(path, "--clipboard", "--input")
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		return fmt.Errorf("no clipboard utility found (install wl-copy, xclip, or xsel)")
	case "windows":
		cmd := exec.Command("clip")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported OS for clipboard: %s", runtime.GOOS)
	}
}
